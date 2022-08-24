package main

import (
	"fmt"
	"unsafe"
)

const (
	// Maximum number of key/elem pairs a bucket can hold.
	bucketCntBits = 3
	bucketCnt     = 1 << bucketCntBits
)

// A bucket for a Go map.
type bmap struct {
	// tophash generally contains the top byte of the hash value
	// for each key in this bucket. If tophash[0] < minTopHash,
	// tophash[0] is a bucket evacuation state instead.
	tophash [bucketCnt]uint8
	// Followed by bucketCnt keys and then bucketCnt elems.
	// NOTE: packing all the keys together and then all the elems together makes the
	// code a bit more complicated than alternating key/elem/key/elem/... but it allows
	// us to eliminate padding which would be needed for, e.g., map[int64]int8.
	// Followed by an overflow pointer.
}

// Should be a built-in for unsafe.Pointer?
//
//go:nosplit
func add(p unsafe.Pointer, x uintptr) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + x)
}

type emptyInterface struct {
	_type unsafe.Pointer
	value unsafe.Pointer
}

// A header for a Go map.
type hmap struct {
	// Note: the format of the hmap is also encoded in cmd/compile/internal/gc/reflect.go.
	// Make sure this stays in sync with the compiler's definition.
	count     int // # live cells == size of map.  Must be first (used by len() builtin)
	flags     uint8
	B         uint8  // log_2 of # of buckets (can hold up to loadFactor * 2^B items)
	noverflow uint16 // approximate number of overflow buckets; see incrnoverflow for details
	hash0     uint32 // hash seed

	buckets    unsafe.Pointer // array of 2^B Buckets. may be nil if count==0.
	oldbuckets unsafe.Pointer // previous bucket array of half the size, non-nil only when growing
	nevacuate  uintptr        // progress counter for evacuation (buckets less than this have been evacuated)

	extra *mapextra // optional fields
}

// mapextra holds fields that are not present on all maps.
type mapextra struct {
	// If both key and elem do not contain pointers and are inline, then we mark bucket
	// type as containing no pointers. This avoids scanning such maps.
	// However, bmap.overflow is a pointer. In order to keep overflow buckets
	// alive, we store pointers to all overflow buckets in hmap.extra.overflow and hmap.extra.oldoverflow.
	// overflow and oldoverflow are only used if key and elem do not contain pointers.
	// overflow contains overflow buckets for hmap.buckets.
	// oldoverflow contains overflow buckets for hmap.oldbuckets.
	// The indirection allows to store a pointer to the slice in hiter.
	overflow    *[]*bmap
	oldoverflow *[]*bmap

	// nextOverflow holds a pointer to a free overflow bucket.
	nextOverflow *bmap
}

func mapTypeAndValue(m interface{}) (*maptype, *hmap) {
	ei := (*emptyInterface)(unsafe.Pointer(&m))
	return (*maptype)(ei._type), (*hmap)(ei.value)
}

type maptype struct {
	typ        _type
	key        *_type
	elem       *_type
	bucket     *_type // internal type representing a hash bucket
	hasher     func(unsafe.Pointer, uintptr) uintptr
	keysize    uint8  // size of key slot
	elemsize   uint8  // size of elem slot
	bucketsize uint16 // size of bucket
	flags      uint32
}

// typeAlg is also copied/used in reflect/type.go.
// keep them in sync.
type typeAlg struct {
	// function for hashing objects of this type
	// (ptr to object, seed) -> hash
	hash func(unsafe.Pointer, uintptr) uintptr
	// function for comparing objects of this type
	// (ptr to object A, ptr to object B) -> ==?
	equal func(unsafe.Pointer, unsafe.Pointer) bool
}

type tflag uint8
type nameOff int32
type typeOff int32
type _type struct {
	size       uintptr
	ptrdata    uintptr // size of memory prefix holding all pointers
	hash       uint32
	tflag      tflag
	align      uint8
	fieldalign uint8
	kind       uint8
	alg        *typeAlg
	// gcdata stores the GC type data for the garbage collector.
	// If the KindGCProg bit is set in kind, gcdata is a GC program.
	// Otherwise it is a ptrmask bitmap. See mbitmap.go for details.
	gcdata    *byte
	str       nameOff
	ptrToThis typeOff
}

const PtrSize = 4 << (^uintptr(0) >> 63)

// bucketShift returns 1<<b, optimized for code generation.
func bucketShift(b uint8) uintptr {
	// Masking the shift amount allows overflow checks to be elided.
	return uintptr(1) << (b & (PtrSize*8 - 1))
}

var (
	// data offset should be the size of the bmap struct, but needs to be
	// aligned correctly. For amd64p32 this means 64-bit alignment
	// even though pointers are 32 bit.
	dataOffset = unsafe.Offsetof(struct {
		b bmap
		v int64
	}{}.v)
)

func noescape(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	return unsafe.Pointer(x ^ 0)
}

//var (
//	dataOffset = 8
//)

func (b *bmap) keys() unsafe.Pointer {
	return add(unsafe.Pointer(b), uintptr(dataOffset))
}

// bucketMask returns 1<<b - 1, optimized for code generation.
func bucketMask(b uint8) uintptr {
	return bucketShift(b) - 1
}

func (b *bmap) overflow(t *maptype) *bmap {
	return *(**bmap)(add(unsafe.Pointer(b), uintptr(t.bucketsize)-PtrSize))
}

// isEmpty reports whether the given tophash array entry represents an empty bucket entry.
func isEmpty(x uint8) bool {
	return x <= 1
}

func main() {
	abc := make(map[int]int, 100)
	for i := 0; i < 100; i++ {
		abc[i] = i * i
	}

	t, h := mapTypeAndValue(abc)

	// 查找 value 的过程
	key := 40
	hash := t.hasher(noescape(unsafe.Pointer(&key)), uintptr(h.hash0))

	m := bucketMask(h.B)
	var b *bmap
	b = (*bmap)(add(h.buckets, (hash&m)*uintptr(t.bucketsize)))
	for ; b != nil; b = b.overflow(t) { // 这段代码可以去掉，虽然不知道是什么作用
		for i, k := uintptr(0), b.keys(); i < bucketCnt; i, k = i+1, add(k, 8) {
			if *(*uint64)(k) == uint64(key) && !isEmpty(b.tophash[i]) {
				value := add(unsafe.Pointer(b), dataOffset+bucketCnt*8+i*uintptr(t.elemsize))
				fmt.Println(value, *(*uint64)(value))
			}
		}
	}
}

