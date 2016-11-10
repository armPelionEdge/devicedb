package devicedb

import (
    "encoding/json"
    "encoding/gob"
    "bytes"
)

type SiblingSet struct {
    siblings map[*Sibling]bool
}

func NewSiblingSet(siblings map[*Sibling]bool) *SiblingSet {
    return &SiblingSet{siblings}
}

func (siblingSet *SiblingSet) Add(sibling *Sibling) *SiblingSet {
    siblingSet.siblings[sibling] = true
    
    return siblingSet
}

func (siblingSet *SiblingSet) Delete(sibling *Sibling) *SiblingSet {
    delete(siblingSet.siblings, sibling)
    
    return siblingSet
}

func (siblingSet *SiblingSet) Has(sibling *Sibling) bool {
    _, ok := siblingSet.siblings[sibling]
    
    return ok
}

func (siblingSet *SiblingSet) Size() int {
    return len(siblingSet.siblings)
}

func (siblingSet *SiblingSet) Value() []byte {
    if siblingSet.Size() != 1 || siblingSet.IsTombstoneSet() {
        return nil
    }

    for sibling, _ := range siblingSet.siblings {
        return sibling.Value()
    }
    
    return nil
}

func (siblingSet *SiblingSet) Sync(otherSiblingSet *SiblingSet) *SiblingSet {
    newSiblingSet := NewSiblingSet(map[*Sibling]bool{ })
    
    for mySibling, _ := range siblingSet.siblings {
        newSiblingSet.Add(mySibling)
        
        for theirSibling, _ := range otherSiblingSet.siblings {
            if mySibling.Clock().HappenedBefore(theirSibling.Clock()) || mySibling.Clock().Equals(theirSibling.Clock()) {
                newSiblingSet.Delete(mySibling)
            }
        }
    }
    
    for theirSibling, _ := range otherSiblingSet.siblings {
        newSiblingSet.Add(theirSibling)
        
        for mySibling, _ := range siblingSet.siblings {
            if theirSibling.Clock().HappenedBefore(mySibling.Clock()) {
                newSiblingSet.Delete(theirSibling)
            }
        }
    }
    
    return newSiblingSet
}

func (siblingSet *SiblingSet) Join() map[string]uint64 {
    collectiveClock := make(map[string]uint64)
    
    for sibling, _ := range siblingSet.siblings {
        for _, replica := range sibling.Clock().Replicas() {
            maxDot := sibling.Clock().MaxDot(replica)
            
            if count, ok := collectiveClock[replica]; !ok || count < maxDot {
                collectiveClock[replica] = maxDot
            }
        }
    }
    
    return collectiveClock
}

func (siblingSet *SiblingSet) Discard(clock *DVV) *SiblingSet {
    newSiblingSet := NewSiblingSet(map[*Sibling]bool{})
    
    for sibling, _ := range siblingSet.siblings {
        if !sibling.Clock().HappenedBefore(clock) {
            newSiblingSet.Add(sibling)
        }
    }
    
    return newSiblingSet
}

func (siblingSet *SiblingSet) Event(contextClock map[string]uint64, replica string) *DVV {
    var s uint64
    
    if count, ok := contextClock[replica]; ok {
        s = count
    }
    
    for sibling, _ := range siblingSet.siblings {
        if maxDot := sibling.Clock().MaxDot(replica); s < maxDot {
            s = maxDot
        }
    }
    
    return NewDVV(NewDot(replica, s+1), contextClock)
}

func (siblingSet *SiblingSet) IsTombstoneSet() bool {
    for sibling, _ := range siblingSet.siblings {
        if !sibling.IsTombstone() {
            return false
        }
    }
    
    return true
}

func (siblingSet *SiblingSet) CanPurge(timestampCutoff uint64) bool {
    for sibling := range siblingSet.Iter() {
        if !sibling.IsTombstone() || sibling.Timestamp() >= timestampCutoff {
            return false
        }
    }
    
    return true
}

func (siblingSet *SiblingSet) GetOldestTombstone() *Sibling {
    var oldestTombstone *Sibling
    
    for sibling, _ := range siblingSet.siblings {
        if sibling.IsTombstone() {
            if oldestTombstone == nil {
                oldestTombstone = sibling
            } else if oldestTombstone.Timestamp() > sibling.Timestamp() {
                oldestTombstone = sibling
            }
        }
    }
    
    return oldestTombstone
}

func (siblingSet *SiblingSet) Iter() <-chan *Sibling {
    ch := make(chan *Sibling)
    
    go func() {
        for sibling, _ := range siblingSet.siblings {
            ch <- sibling
        }
        
        close(ch)
    } ()
    
    return ch
}

func (siblingSet *SiblingSet) Hash(key []byte) Hash {
    if siblingSet == nil {
        return Hash{[2]uint64{ 0, 0 }}
    }
    
    var result Hash
    
    for sibling := range siblingSet.Iter() {
        result = result.Xor(sibling.Hash())
    }

    if result.Low() != 0 && result.High() != 0 {
        result = result.Xor(NewHash(key))
    }
    
    return result
}

func (siblingSet *SiblingSet) MarshalBinary() ([]byte, error) {
    var encoding bytes.Buffer
    
    encoder := gob.NewEncoder(&encoding)
    
    err := encoder.Encode(siblingSet.siblings)
    
    return encoding.Bytes(), err
}

func (siblingSet *SiblingSet) UnmarshalBinary(data []byte) error {
    var siblings map[*Sibling]bool
    
    encodedBuffer := bytes.NewBuffer(data)
    decoder := gob.NewDecoder(encodedBuffer)
    
    err := decoder.Decode(&siblings)
    
    siblingSet.siblings = siblings
    
    return err
}

func (siblingSet *SiblingSet) Encode() []byte {
    b, _ := siblingSet.MarshalJSON()
    return b
    var encoding bytes.Buffer
    
    encoder := gob.NewEncoder(&encoding)
    
    _ = encoder.Encode(siblingSet)
    
    return encoding.Bytes()
}

func (siblingSet *SiblingSet) Decode(encodedSiblingSet []byte) error {
    return siblingSet.UnmarshalJSON(encodedSiblingSet)
    encodedBuffer := bytes.NewBuffer(encodedSiblingSet)
    decoder := gob.NewDecoder(encodedBuffer)
    
    err := decoder.Decode(siblingSet)
    
    return err
}

func (siblingSet *SiblingSet) MarshalJSON() ([]byte, error) {
    siblingList := make([]*Sibling, 0, len(siblingSet.siblings))
    
    for ss, _ := range siblingSet.siblings {
        siblingList = append(siblingList, ss)
    }
    
    return json.Marshal(siblingList)
}

func (siblingSet *SiblingSet) UnmarshalJSON(data []byte) error {
    siblingList := make([]*Sibling, 0)
    err := json.Unmarshal(data, &siblingList)
    
    if err != nil {
        return err
    }
    
    siblingSet.siblings = make(map[*Sibling]bool, len(siblingList))
    
    for _, ss := range siblingList {
        siblingSet.siblings[ss] = true
    }
    
    return nil
}
