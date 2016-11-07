package devicedb_test

import (
	. "devicedb"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
    
    "fmt"
)

func newStorageDriver() StorageDriver {
    return NewLevelDBStorageDriver("/tmp/testdevicedb", nil)
}

var _ = Describe("StorageEngine", func() {
    Describe("#Open", func() {
        It("Open should not return any error", func() {
            storageDriver := newStorageDriver()
            
            defer storageDriver.Close()
            
            Expect(storageDriver.Open()).Should(Succeed())
        })
        
        It("Calling open twice should implictly close then open a new database", func() {
            storageDriver := newStorageDriver()
            
            defer storageDriver.Close()
            
            Expect(storageDriver.Open()).Should(Succeed())
            Expect(storageDriver.Open()).Should(Succeed())
        })
    })
    
    Describe("#Close", func() {
        It("Close should not return any error", func() {
            storageDriver := newStorageDriver()
            
            Expect(storageDriver.Open()).Should(Succeed())
            Expect(storageDriver.Close()).Should(Succeed())
        })
        
        It("Calling close twice should work", func() {
            storageDriver := newStorageDriver()
            
            Expect(storageDriver.Open()).Should(Succeed())
            Expect(storageDriver.Close()).Should(Succeed())
            Expect(storageDriver.Close()).Should(Succeed())
        })
    })
    
    Describe("Driver", func() {
        It("Get should return an array of values corresponding to the input keys. If an input key is nil then the corresponding value should be nil. A nil input array results in an empty value array", func() {
            keyCount := 10000
            storageDriver := newStorageDriver()
            defer storageDriver.Close()
            storageDriver.Open()
            
            batch := NewBatch()
            
            for i := 0; i < keyCount; i += 1 {
                batch.Put([]byte(fmt.Sprintf("key%05d", i)), []byte(fmt.Sprintf("value%05d", i)))
            }
            
            Expect(storageDriver.Batch(batch)).Should(Succeed())
            
            Expect(storageDriver.Get([][]byte{ 
                []byte("key00000"),
                []byte("key00001"),
                []byte("key00002"),
            })).Should(Equal([][]byte{
                []byte("value00000"),
                []byte("value00001"),
                []byte("value00002"),
            }))
            
            Expect(storageDriver.Get([][]byte{ 
                []byte("keyD"),
                []byte("keyE"),
                []byte("keyF"),
            })).Should(Equal([][]byte{
                nil,
                nil,
                nil,
            }))
            
            Expect(storageDriver.Get([][]byte{ 
                []byte("key00000"),
                []byte("key00001"),
                nil,
            })).Should(Equal([][]byte{
                []byte("value00000"),
                []byte("value00001"),
                nil,
            }))
            
            Expect(storageDriver.Get(nil)).Should(Equal([][]byte{ }))
            
            iterator, err := storageDriver.GetMatches([][]byte{
                []byte("key"),
            })
            
            Expect(err).Should(Succeed())
            
            for i := 0; i < keyCount; i += 1 {
                Expect(iterator.Next()).Should(BeTrue())
                Expect(iterator.Prefix()).Should(Equal([]byte("key")))
                Expect(iterator.Key()).Should(Equal([]byte(fmt.Sprintf("key%05d", i))))
                Expect(iterator.Value()).Should(Equal([]byte(fmt.Sprintf("value%05d", i))))
            }
            
            Expect(iterator.Next()).Should(BeFalse())
            Expect(iterator.Prefix()).Should(BeNil())
            Expect(iterator.Key()).Should(BeNil())
            Expect(iterator.Value()).Should(BeNil())
            Expect(iterator.Error()).Should(BeNil())
            iterator.Release()
            Expect(iterator.Next()).Should(BeFalse())
            Expect(iterator.Prefix()).Should(BeNil())
            Expect(iterator.Key()).Should(BeNil())
            Expect(iterator.Value()).Should(BeNil())
            Expect(iterator.Error()).Should(BeNil())
            
            iterator, err = storageDriver.GetMatches([][]byte{
                []byte("key000"),
                []byte("key022"),
                []byte("key044"),
            })
            
            Expect(err).Should(Succeed())
            
            for i := 0; i <= 99; i += 1 {
                Expect(iterator.Next()).Should(BeTrue())
                Expect(iterator.Prefix()).Should(Equal([]byte("key000")))
                Expect(iterator.Key()).Should(Equal([]byte(fmt.Sprintf("key%05d", i))))
                Expect(iterator.Value()).Should(Equal([]byte(fmt.Sprintf("value%05d", i))))
            }
            
            for i := 2200; i <= 2299; i += 1 {
                Expect(iterator.Next()).Should(BeTrue())
                Expect(iterator.Prefix()).Should(Equal([]byte("key022")))
                Expect(iterator.Key()).Should(Equal([]byte(fmt.Sprintf("key%05d", i))))
                Expect(iterator.Value()).Should(Equal([]byte(fmt.Sprintf("value%05d", i))))
            }
            
            for i := 4400; i <= 4499; i += 1 {
                Expect(iterator.Next()).Should(BeTrue())
                Expect(iterator.Prefix()).Should(Equal([]byte("key044")))
                Expect(iterator.Key()).Should(Equal([]byte(fmt.Sprintf("key%05d", i))))
                Expect(iterator.Value()).Should(Equal([]byte(fmt.Sprintf("value%05d", i))))
            }
            
            Expect(iterator.Next()).Should(BeFalse())
            Expect(iterator.Prefix()).Should(BeNil())
            Expect(iterator.Key()).Should(BeNil())
            Expect(iterator.Value()).Should(BeNil())
            Expect(iterator.Error()).Should(BeNil())
            iterator.Release()
            Expect(iterator.Next()).Should(BeFalse())
            Expect(iterator.Prefix()).Should(BeNil())
            Expect(iterator.Key()).Should(BeNil())
            Expect(iterator.Value()).Should(BeNil())
            Expect(iterator.Error()).Should(BeNil())
            
            iterator, err = storageDriver.GetRange([]byte("key055"), []byte("key099"))
            
            Expect(err).Should(Succeed())
            
            for i := 5500; i <= 9899; i += 1 {
                Expect(iterator.Next()).Should(BeTrue())
                Expect(iterator.Key()).Should(Equal([]byte(fmt.Sprintf("key%05d", i))))
                Expect(iterator.Value()).Should(Equal([]byte(fmt.Sprintf("value%05d", i))))
            }
            
            Expect(iterator.Next()).Should(BeFalse())
            Expect(iterator.Key()).Should(BeNil())
            Expect(iterator.Value()).Should(BeNil())
            Expect(iterator.Error()).Should(BeNil())
            iterator.Release()
            Expect(iterator.Next()).Should(BeFalse())
            Expect(iterator.Key()).Should(BeNil())
            Expect(iterator.Value()).Should(BeNil())
            Expect(iterator.Error()).Should(BeNil())
            
            batch = NewBatch()
            
            for i := 0; i < keyCount; i += 1 {
                batch.Delete([]byte(fmt.Sprintf("key%05d", i)))
            }
            
            Expect(storageDriver.Batch(batch)).Should(Succeed())
            
            for i := 0; i < keyCount; i += 1 {
                Expect(storageDriver.Get([][]byte{ []byte(fmt.Sprintf("key%05d", i)) })).Should(Equal([][]byte{ nil }))
            }
        })
    })
})