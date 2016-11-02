package io

import (
    "fmt"
    "io"
    "net"
    "errors"
    "net/http"
    "crypto/tls"
    "crypto/x509"
    "encoding/json"
    "time"
    "strconv"
    "github.com/gorilla/mux"
    "github.com/gorilla/websocket"
    "devicedb/storage"
    "devicedb/strategies"
    "devicedb/sync"
)

type Shared struct {
}

func (shared *Shared) ShouldReplicateOutgoing(peerID string) bool {
    return true
}

func (shared *Shared) ShouldReplicateIncoming(peerID string) bool {
    return true
}

type Cloud struct {
}

func (cloud *Cloud) ShouldReplicateOutgoing(peerID string) bool {
    return false
}

func (cloud *Cloud) ShouldReplicateIncoming(peerID string) bool {
    return peerID == "cloud"
}

type ServerConfig struct {
    DBFile string
    Port int
    MerkleDepth uint8
    NodeID string
    Peer *Peer
    ServerTLS *tls.Config
}

func (sc *ServerConfig) LoadFromFile(file string) error {
    var jsc JSONServerConfig
    
    err := jsc.LoadFromFile(file)
    
    if err != nil {
        return err
    }
    
    sc.DBFile = jsc.DBFile
    sc.Port = jsc.Port
    sc.MerkleDepth = jsc.MerkleDepth

    rootCAs := x509.NewCertPool()
    
    if !rootCAs.AppendCertsFromPEM([]byte(jsc.TLS.RootCA)) {
        return errors.New("Could not append root CA to chain")
    }
    
    clientCertificate, _ := tls.X509KeyPair([]byte(jsc.TLS.ClientCertificate), []byte(jsc.TLS.ClientKey))
    serverCertificate, _ := tls.X509KeyPair([]byte(jsc.TLS.ServerCertificate), []byte(jsc.TLS.ServerKey))
    clientTLSConfig := &tls.Config{
        Certificates: []tls.Certificate{ clientCertificate },
        RootCAs: rootCAs,
    }
    serverTLSConfig := &tls.Config{
        Certificates: []tls.Certificate{ serverCertificate },
        ClientCAs: rootCAs,
    }
    
    sc.Peer = NewPeer(NewSyncController(uint(jsc.MaxSyncSessions), nil), clientTLSConfig)
    sc.ServerTLS = serverTLSConfig
    
    for _, jsonPeer := range jsc.Peers {
        sc.Peer.Connect(jsonPeer.ID, jsonPeer.Host, jsonPeer.Port)
    }
    
    clientCertX509, _ := x509.ParseCertificate(clientCertificate.Certificate[0])
    serverCertX509, _ := x509.ParseCertificate(serverCertificate.Certificate[0])
    clientCN := clientCertX509.Subject.CommonName
    serverCN := serverCertX509.Subject.CommonName
    
    if len(clientCN) == 0 {
        return errors.New("The common name in the certificate is empty. The node ID must not be empty")
    }
     
    if clientCN != serverCN {
        return errors.New(fmt.Sprintf("Server and client certificates have differing common names(%s and %s). This is the string used to uniquely identify the node.", serverCN, clientCN))
    }
    
    sc.NodeID = clientCN
    
    return nil
}

type Server struct {
    bucketList *BucketList
    httpServer *http.Server
    listener net.Listener
    storageDriver storage.StorageDriver
    port int
    upgrader websocket.Upgrader
    peer *Peer
    serverTLS *tls.Config
    id string
}

func NewServer(serverConfig ServerConfig) (*Server, error) {
    if serverConfig.MerkleDepth < sync.MerkleMinDepth || serverConfig.MerkleDepth > sync.MerkleMaxDepth {
        serverConfig.MerkleDepth = sync.MerkleDefaultDepth
    }
    
    if len(serverConfig.NodeID) == 0 {
        serverConfig.NodeID = "Node"
    }
    
    upgrader := websocket.Upgrader{
    	ReadBufferSize:  1024,
    	WriteBufferSize: 1024,
    }
    
    storageDriver := storage.NewLevelDBStorageDriver(serverConfig.DBFile, nil)
    nodeID := serverConfig.NodeID
    server := &Server{ NewBucketList(), nil, nil, storageDriver, serverConfig.Port, upgrader, serverConfig.Peer, serverConfig.ServerTLS, nodeID }
    err := server.storageDriver.Open()
    
    if err != nil {
        log.Errorf("Error creating server: %v", err)
        return nil, err
    }
    
    defaultNode, _ := NewNode(nodeID, storage.NewPrefixedStorageDriver([]byte{ 0 }, storageDriver), serverConfig.MerkleDepth, nil)
    cloudNode, _ := NewNode(nodeID, storage.NewPrefixedStorageDriver([]byte{ 1 }, storageDriver), serverConfig.MerkleDepth, nil) 
    lwwNode, _ := NewNode(nodeID, storage.NewPrefixedStorageDriver([]byte{ 2 }, storageDriver), serverConfig.MerkleDepth, strategies.LastWriterWins)
    
    server.bucketList.AddBucket("default", defaultNode, &Shared{ })
    server.bucketList.AddBucket("lww", lwwNode, &Shared{ })
    server.bucketList.AddBucket("cloud", cloudNode, &Cloud{ })
    
    if server.peer != nil && server.peer.syncController != nil {
        server.peer.syncController.buckets = server.bucketList
    }
    
    return server, nil
}

func (server *Server) Port() int {
    return server.port
}

func (server *Server) Buckets() *BucketList {
    return server.bucketList
}

func (server *Server) Start() error {
    r := mux.NewRouter()
    
    r.HandleFunc("/{bucket}/values", func(w http.ResponseWriter, r *http.Request) {
        bucket := mux.Vars(r)["bucket"]
        
        if !server.bucketList.HasBucket(bucket) {
            log.Warningf("POST /{bucket}/values: Invalid bucket")
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusNotFound)
            io.WriteString(w, string(EInvalidBucket.JSON()) + "\n")
            
            return
        }
        
        keys := make([]string, 0)
        decoder := json.NewDecoder(r.Body)
        err := decoder.Decode(&keys)
        
        if err != nil {
            log.Warningf("POST /{bucket}/values: %v", err)
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusBadRequest)
            io.WriteString(w, string(EInvalidKey.JSON()) + "\n")
            
            return
        }
        
        if len(keys) == 0 {
            log.Warningf("POST /{bucket}/values: Empty keys array")
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusBadRequest)
            io.WriteString(w, string(EInvalidKey.JSON()) + "\n")
            
            return
        }
        
        byteKeys := make([][]byte, 0, len(keys))
        
        for _, k := range keys {
            if len(k) == 0 {
                log.Warningf("POST /{bucket}/values: Empty key")
            
                w.Header().Set("Content-Type", "application/json; charset=utf8")
                w.WriteHeader(http.StatusBadRequest)
                io.WriteString(w, string(EInvalidKey.JSON()) + "\n")
                
                return
            }
            
            byteKeys = append(byteKeys, []byte(k))
        }
        
        siblingSets, err := server.bucketList.Get(bucket).Node.Get(byteKeys)
        
        if err != nil {
            log.Warningf("POST /{bucket}/values: Internal server error")
        
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusInternalServerError)
            io.WriteString(w, string(err.(DBerror).JSON()) + "\n")
            
            return
        }
        
        siblingSetsJSON, _ := json.Marshal(siblingSets)
        
        w.Header().Set("Content-Type", "application/json; charset=utf8")
        w.WriteHeader(http.StatusOK)
        io.WriteString(w, string(siblingSetsJSON))
    }).Methods("POST")
    
    r.HandleFunc("/{bucket}/matches", func(w http.ResponseWriter, r *http.Request) {
        bucket := mux.Vars(r)["bucket"]
        
        if !server.bucketList.HasBucket(bucket) {
            log.Warningf("POST /{bucket}/matches: Invalid bucket")
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusNotFound)
            io.WriteString(w, string(EInvalidBucket.JSON()) + "\n")
            
            return
        }
        
        keys := make([]string, 0)
        decoder := json.NewDecoder(r.Body)
        err := decoder.Decode(&keys)
        
        if err != nil {
            log.Warningf("POST /{bucket}/matches: %v", err)
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusBadRequest)
            io.WriteString(w, string(EInvalidKey.JSON()) + "\n")
            
            return
        }
        
        if len(keys) == 0 {
            log.Warningf("POST /{bucket}/matches: Empty keys array")
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusBadRequest)
            io.WriteString(w, string(EInvalidKey.JSON()) + "\n")
            
            return
        }
        
        byteKeys := make([][]byte, 0, len(keys))
        
        for _, k := range keys {
            if len(k) == 0 {
                log.Warningf("POST /{bucket}/matches: Empty key")
            
                w.Header().Set("Content-Type", "application/json; charset=utf8")
                w.WriteHeader(http.StatusBadRequest)
                io.WriteString(w, string(EInvalidKey.JSON()) + "\n")
                
                return
            }
            
            byteKeys = append(byteKeys, []byte(k))
        }
        
        ssIterator, err := server.bucketList.Get(bucket).Node.GetMatches(byteKeys)
        
        if err != nil {
            log.Warningf("POST /{bucket}/matches: Internal server error")
        
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusInternalServerError)
            io.WriteString(w, string(err.(DBerror).JSON()) + "\n")
            
            return
        }
        
        defer ssIterator.Release()
    
        flusher, _ := w.(http.Flusher)
        
        w.Header().Set("Content-Type", "application/json; charset=utf8")
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.WriteHeader(http.StatusOK)
        
        for ssIterator.Next() {
            key := ssIterator.Key()
            prefix := ssIterator.Prefix()
            nextSiblingSet := ssIterator.Value()
            siblingSetsJSON, _ := json.Marshal(nextSiblingSet)
            
            _, err = fmt.Fprintf(w, "%s\n%s\n%s\n", string(prefix), string(key), string(siblingSetsJSON))
            flusher.Flush()
            
            if err != nil {
                return
            }
        }
    }).Methods("POST")
    
    r.HandleFunc("/{bucket}/batch", func(w http.ResponseWriter, r *http.Request) {
        bucket := mux.Vars(r)["bucket"]
        
        if !server.bucketList.HasBucket(bucket) {
            log.Warningf("POST /{bucket}/batch: Invalid bucket")
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusNotFound)
            io.WriteString(w, string(EInvalidBucket.JSON()) + "\n")
            
            return
        }
        
        var updateBatch UpdateBatch
        err := updateBatch.FromJSON(r.Body)
        
        if err != nil {
            log.Warningf("POST /{bucket}/batch: %v", err)
            
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusBadRequest)
            io.WriteString(w, string(EInvalidBatch.JSON()) + "\n")
            
            return
        }
        
        updatedSiblingSets, err := server.bucketList.Get(bucket).Node.Batch(&updateBatch)
        
        if err != nil {
            log.Warningf("POST /{bucket}/batch: Internal server error")
        
            w.Header().Set("Content-Type", "application/json; charset=utf8")
            w.WriteHeader(http.StatusInternalServerError)
            io.WriteString(w, string(err.(DBerror).JSON()) + "\n")
            
            return
        }
    
        if server.peer != nil {
            for key, siblingSet := range updatedSiblingSets {
                server.peer.SyncController().BroadcastUpdate(key, siblingSet, 0)
            }
        }
        
        w.Header().Set("Content-Type", "application/json; charset=utf8")
        w.WriteHeader(http.StatusOK)
        io.WriteString(w, "\n")
    }).Methods("POST")
    
    r.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
        if server.peer == nil {
            // log error
            
            return
        }
        
        conn, err := server.upgrader.Upgrade(w, r, nil)
        
        if err != nil {
            return
        }
        
        server.peer.Accept(conn)
    }).Methods("GET")
    
    server.httpServer = &http.Server{
        Handler: r,
        WriteTimeout: 15 * time.Second,
        ReadTimeout: 15 * time.Second,
    }
    
    var listener net.Listener
    var err error

    if server.serverTLS == nil {
        listener, err = net.Listen("tcp", "0.0.0.0:" + strconv.Itoa(server.Port()))
    } else {
        server.serverTLS.ClientAuth = tls.VerifyClientCertIfGiven
        listener, err = tls.Listen("tcp", "0.0.0.0:" + strconv.Itoa(server.Port()), server.serverTLS)
    }
    
    if err != nil {
        log.Errorf("Error listening on port: %d", server.port)
        
        server.Stop()
        
        return err
    }
    
    err = server.storageDriver.Open()
    
    if err != nil {
        log.Errorf("Error opening storage driver: %v", err)
        
        return EStorage
    }
    
    server.listener = listener

    log.Infof("Node %s listening on port %d", server.id, server.port)
    return server.httpServer.Serve(server.listener)
}

func (server *Server) Stop() error {
    if server.listener != nil {
        server.listener.Close()
    }
    
    server.storageDriver.Close()
    
    return nil
}