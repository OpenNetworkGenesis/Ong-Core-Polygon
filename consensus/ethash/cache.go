package ethash

import (
	"encoding/binary"
	"fmt"
	"hash"
	"sync"

	"golang.org/x/crypto/sha3"
)

type hashRead interface {
	hash.Hash
	Read(b []byte) (int, error)
}

// Cache is a 16 MB pseudorandom cache.
type Cache struct {
	cacheSize   uint32
	datasetSize int
	cache       []uint32
	sha512      hashRead
	sha256      hashRead
	mix         [16]uint32
}

func newCache(epoch int) *Cache {
	cacheSize := getCacheSizeByEpoch(epoch)
	datasetSize := getDatasetSizeByEpoch(epoch)
	seed := getSeedHashByEpoch(epoch)

	c := &Cache{
		sha512:      sha3.NewLegacyKeccak512().(hashRead),
		sha256:      sha3.NewLegacyKeccak256().(hashRead),
		datasetSize: int(datasetSize),
	}

	c.mkcache(int(cacheSize), seed)
	c.cacheSize = uint32(len(c.cache))
	return c
}

func (c *Cache) calcDatasetItem(i uint32) []uint32 {
	n := c.cacheSize
	m := uint32(len(c.cache) / 16)
	r := hashBytes / wordBytes

	copy(c.mix[:], c.cache[(i*16)%n:])
	c.mix[0] ^= i
	c.sha512Int(c.mix[:])

	for j := 0; j < datasetParents; j++ {
		cacheIndex := (fnvOp(i^uint32(j), c.mix[j%r]) % m) * 16

		aux := c.cache[cacheIndex : cacheIndex+16]
		for o := 0; o < 16; o++ {
			c.mix[o] = fnvOp(c.mix[o], aux[o])
		}
	}

	c.sha512Int(c.mix[:])
	return c.mix[:]
}

func (c *Cache) sha512NoCopy(r []byte, p []byte) {
	c.sha512.Reset()
	c.sha512.Write(p)
	n, _ := c.sha512.Read(r)
	if n != 64 {
		panic("wrong size")
	}
}

func (c *Cache) sha512Aux(p []byte) []byte {
	c.sha512.Reset()
	c.sha512.Write(p)
	return c.sha512.Sum(nil)
}

func (c *Cache) sha256Aux(p []byte) []byte {
	c.sha256.Reset()
	c.sha256.Write(p)
	return c.sha256.Sum(nil)
}

func (c *Cache) sha512Int(p []uint32) {
	aux := make([]byte, 4)

	c.sha512.Reset()
	for _, i := range p {
		binary.LittleEndian.PutUint32(aux, i)
		c.sha512.Write(aux)
	}
	res := c.sha512.Sum(nil)
	for i := 0; i < len(p); i++ {
		p[i] = binary.LittleEndian.Uint32(res[i*4:])
	}
}

var bytePool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 128)
	},
}

func extendByteSlice(b []byte, needLen int) []byte {
	b = b[:cap(b)]
	if n := needLen - cap(b); n > 0 {
		b = append(b, make([]byte, n)...)
	}
	return b[:needLen]
}

func (c *Cache) mkcache(cacheSize int, seed []byte) {
	n := cacheSize / hashBytes

	res := bytePool.Get().([]byte)
	res = extendByteSlice(res, n*hashBytes)

	c.sha512NoCopy(res[0:64], seed)

	for i := 1; i < n; i++ {
		indx := (i - 1) * hashBytes
		c.sha512NoCopy(res[i*hashBytes:i*hashBytes+64], res[indx:indx+hashBytes])
	}

	xorTmp := make([]byte, hashBytes)

	for j := 0; j < cacheRounds; j++ {
		for i := 0; i < n; i++ {
			indx := i * hashBytes
			offset := ((i - 1 + n) % n) * hashBytes

			v := int(binary.LittleEndian.Uint32(res[indx:indx+hashBytes])) % n * hashBytes

			xorBytes(xorTmp, res[offset:offset+hashBytes], res[v:v+hashBytes])
			c.sha512NoCopy(res[indx:indx+64], xorTmp)
		}
	}

	// Convert bytes to words
	resInt := make([]uint32, n*16)
	for i := 0; i < len(resInt); i++ {
		resInt[i] = binary.LittleEndian.Uint32(res[i*4:])
	}

	bytePool.Put(res)
	c.cache = resInt
}

func (c *Cache) hashimoto(header []byte, nonce uint64) ([]byte, []byte) {
	return hashimoto(header, nonce, c.datasetSize, c.sha512Aux, c.sha256Aux, c.calcDatasetItem)
}

func xorBytes(res, a, b []byte) {
	if len(a) != len(b) {
		panic(fmt.Sprintf("length of byte slices is not equivalent: %d != %d", len(a), len(b)))
	}
	for i := range a {
		res[i] = a[i] ^ b[i]
	}
}
