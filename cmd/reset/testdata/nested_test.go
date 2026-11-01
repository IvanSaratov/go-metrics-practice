package fixture

import (
	"reflect"
	"testing"

	"example.com/resetfixture/child"
)

func TestResetPointers(t *testing.T) {
	number, text, flag, named := 42, "value", true, Count(7)
	slice := make([]int, 3, 8)
	backing := &slice[0]
	values := map[string]int{"value": 1}
	mapAlias := values
	indirect := &number
	value := Nested{
		Number: &number, Text: &text, Flag: &flag, Named: &named,
		Slice: &slice, Map: &values, Indirect: &indirect,
	}
	value.Reset()
	if value.Number != &number || value.Text != &text || value.Flag != &flag || value.Named != &named ||
		value.Slice != &slice || value.Map != &values || value.Indirect != &indirect || indirect != &number {
		t.Fatal("указатели заменены")
	}
	if number != 0 || text != "" || flag || named != 0 {
		t.Fatal("значения за указателями не сброшены")
	}
	if len(slice) != 0 || cap(slice) != 8 || &slice[:cap(slice)][0] != backing {
		t.Fatal("память среза за указателем не сохранена")
	}
	if values == nil || len(values) != 0 || len(mapAlias) != 0 {
		t.Fatal("мапа за указателем не очищена")
	}
	values["after"] = 2
	if mapAlias["after"] != 2 {
		t.Fatal("мапа за указателем заменена")
	}
}

func TestResetNestedStructs(t *testing.T) {
	plain := &Unmarked{Value: 5}
	own := &Second{Value: "own"}
	external := &child.Counter{Value: 9}
	manual := &Manual{Value: 3}
	value := Nested{
		Plain: Unmarked{Value: 4}, PlainPointer: plain,
		Own: Second{Value: "own"}, OwnPointer: own,
		External: child.Counter{Value: 8}, ExternalPtr: external, ExternalCount: 10,
		Manual: Manual{Value: 2}, ManualPointer: manual,
	}
	value.Reset()
	if value.Plain.Value != 4 || value.PlainPointer != plain || plain.Value != 5 {
		t.Fatal("структура без Reset изменилась")
	}
	if value.Own.Value != "" || value.OwnPointer != own || own.Value != "" ||
		value.External.Value != 0 || value.ExternalPtr != external || external.Value != 0 || value.ExternalCount != 0 {
		t.Fatal("вложенные структуры не сброшены или их указатели заменены")
	}
	if value.Manual.Value != 0 || value.Manual.Calls != 1 || value.ManualPointer != manual || manual.Value != 0 || manual.Calls != 1 {
		t.Fatal("ручной Reset должен вызываться ровно один раз")
	}
}

func TestResetArraysAndNilValues(t *testing.T) {
	first, second := 1, 2
	slice := []int{3, 4}
	value := Nested{
		Array: [2]*int{&first, &second}, Slices: [2][]int{slice, nil},
		Matrix:  [2][2]int{{1, 2}, {3, 4}},
		Channel: make(chan int), Function: func() {}, Interface: "value",
	}
	value.Reset()
	if first != 0 || second != 0 || value.Array != [2]*int{&first, &second} {
		t.Fatal("указатели внутри массива сброшены неверно")
	}
	if value.Slices[0] == nil || len(value.Slices[0]) != 0 ||
		cap(value.Slices[0]) != cap(slice) || &value.Slices[0][:cap(slice)][0] != &slice[0] || value.Slices[1] != nil {
		t.Fatal("срезы внутри массива сброшены неверно")
	}
	if value.Matrix != [2][2]int{} || value.Channel != nil || value.Function != nil || value.Interface != nil {
		t.Fatal("остальные поля не сброшены")
	}

	var missing *Nested
	missing.Reset()
	var nilSlice []int
	var nilMap map[string]int
	var nilPointer *int
	value = Nested{Slice: &nilSlice, Map: &nilMap, Indirect: &nilPointer}
	value.Reset()
	if nilSlice != nil || nilMap != nil || nilPointer != nil || value.Number != nil || value.OwnPointer != nil || value.ManualPointer != nil {
		t.Fatal("nil должен остаться nil")
	}
}

func TestResetEmbeddedAndRecursive(t *testing.T) {
	manual := &Manual{Value: 5}
	embedded := Embedded{Manual: manual, Text: "value"}
	embedded.Reset()
	if embedded.Manual != manual || manual.Value != 0 || manual.Calls != 1 || embedded.Text != "" {
		t.Fatal("встроенное поле или собственное поле не сброшено")
	}
	empty := Embedded{Text: "value"}
	empty.Reset()
	if !reflect.DeepEqual(Embedded{}, empty) {
		t.Fatal("nil встроенного поля должен сохраниться")
	}
	tail := &Node{Value: 2}
	head := &Node{Value: 1, Next: tail}
	head.Reset()
	if head.Value != 0 || head.Next != tail || tail.Value != 0 || tail.Next != nil {
		t.Fatal("цепочка вложенных структур сброшена неверно")
	}
}
