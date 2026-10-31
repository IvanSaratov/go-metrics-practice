// Сгенерировано командой go run ./cmd/reset. Не редактировать.

package models

func (r *Metrics) Reset() {
	if r == nil {
		return
	}
	r.ID = ""
	r.MType = ""
	if r.Delta != nil {
		(*r.Delta) = 0
	}
	if r.Value != nil {
		(*r.Value) = 0
	}
	r.Hash = ""
}
