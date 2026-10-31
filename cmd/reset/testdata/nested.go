package fixture

import "example.com/resetfixture/child"

type CountPointer *Count

type Manual struct {
	Value int
	Calls int
}

func (m *Manual) Reset() {
	m.Value = 0
	m.Calls++
}

// generate:reset
type Nested struct {
	Number        *int
	Text          *string
	Flag          *bool
	Named         CountPointer
	Slice         *[]int
	Map           *map[string]int
	Indirect      **int
	Plain         Unmarked
	PlainPointer  *Unmarked
	Own           Second
	OwnPointer    *Second
	External      child.Counter
	ExternalPtr   *child.Counter
	ExternalCount child.Count
	Manual        Manual
	ManualPointer *Manual
	Array         [2]*int
	Slices        [2][]int
	Matrix        [2][2]int
	Channel       chan int
	Function      func()
	Interface     any
}

// generate:reset
type Embedded struct {
	*Manual
	Text string
}

// generate:reset
type Node struct {
	Value int
	Next  *Node
}
