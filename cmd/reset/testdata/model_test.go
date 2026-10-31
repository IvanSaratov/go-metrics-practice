package fixture

import (
	"reflect"
	"testing"

	"example.com/resetfixture/child"
)

// generate:reset
type TestOnly struct {
	Value int
}

func TestResetBasic(t *testing.T) {
	items := make([]int, 3, 8)
	numbers := Numbers{1, 2}
	values := map[string]int{"before": 1}
	dictionary := Dictionary{"before": 2}
	value := Basic{
		Int: 1, Int8: -2, Int16: 3, Int32: -4, Int64: 5,
		Uint: 1, Uint8: 2, Uint16: 3, Uint32: 4, Uint64: 5, Uintptr: 6,
		Float32: 1.5, Float64: -2.5, Complex64: 1 + 2i, Complex128: 3 + 4i,
		Byte: 'a', Rune: 'я', Text: "text", Enabled: true,
		Count: 9, Alias: 10, Label: "label", Flag: true,
		Items: items, Numbers: numbers, Values: values, Dictionary: dictionary,
	}
	value.Reset()

	want := Basic{
		Items: []int{}, Numbers: Numbers{},
		Values: map[string]int{}, Dictionary: Dictionary{},
	}
	if !reflect.DeepEqual(want, value) {
		t.Fatalf("неверный результат сброса: %+v", value)
	}
	if cap(value.Items) != cap(items) || &value.Items[:cap(value.Items)][0] != &items[0] {
		t.Fatal("изменилась память среза Items")
	}
	if cap(value.Numbers) != cap(numbers) || &value.Numbers[:cap(value.Numbers)][0] != &numbers[0] {
		t.Fatal("изменилась память среза Numbers")
	}
	if len(values) != 0 || len(dictionary) != 0 {
		t.Fatal("исходные мапы не очищены")
	}
	value.Values["after"] = 3
	value.Dictionary["after"] = 4
	if values["after"] != 3 || dictionary["after"] != 4 {
		t.Fatal("мапы заменены новыми")
	}
}

func TestResetNilValues(t *testing.T) {
	var value Basic
	value.Reset()
	if !reflect.DeepEqual(Basic{}, value) {
		t.Fatalf("нулевые значения изменились: %+v", value)
	}
	var missing *Basic
	missing.Reset()
}

func TestResetOtherStructs(t *testing.T) {
	grouped := Grouped{First: "first", Second: "second"}
	grouped.Reset()
	if grouped != (Grouped{}) {
		t.Fatalf("поля в type-блоке не сброшены: %+v", grouped)
	}
	second := Second{Value: "value"}
	second.Reset()
	if second.Value != "" {
		t.Fatal("метод для структуры из второго файла не работает")
	}
	new(Empty).Reset()
	other := child.Counter{Value: 5}
	other.Reset()
	if other.Value != 0 {
		t.Fatal("метод для структуры из вложенного пакета не работает")
	}
	for _, unmarked := range []any{&Unmarked{}, &SimilarMarker{}, &TestOnly{}} {
		if _, ok := unmarked.(interface{ Reset() }); ok {
			t.Fatalf("лишний метод Reset у %T", unmarked)
		}
	}
}
