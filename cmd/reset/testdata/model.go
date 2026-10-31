package fixture

type Count int
type Label string
type Flag bool
type Numbers []int
type Dictionary map[string]int
type CountAlias = Count

// Basic содержит простые значения и коллекции.
// generate:reset
type Basic struct {
	Int        int
	Int8       int8
	Int16      int16
	Int32      int32
	Int64      int64
	Uint       uint
	Uint8      uint8
	Uint16     uint16
	Uint32     uint32
	Uint64     uint64
	Uintptr    uintptr
	Float32    float32
	Float64    float64
	Complex64  complex64
	Complex128 complex128
	Byte       byte
	Rune       rune
	Text       string
	Enabled    bool
	Count      Count
	Alias      CountAlias
	Label      Label
	Flag       Flag
	Items      []int
	Numbers    Numbers
	Values     map[string]int
	Dictionary Dictionary
	_          string
}

type (
	// generate:reset
	Grouped struct {
		First, Second string
	}

	Unmarked struct {
		Value int
	}
)

// generate:reset-other
type SimilarMarker struct {
	Value int
}

// generate:reset
type Empty struct{}
