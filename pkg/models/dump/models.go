package dump

import "gonum.org/v1/gonum/spatial/r3"

// System represents system JSON in the galaxy dump.
type System struct {
	ID64        uint64 `json:"id64"`
	Name        string `json:"name"`
	Coordinates r3.Vec `json:"coords"`
	Bodies      []Body `json:"bodies"`
}

// Body represents individual bodies within system in the galaxy dump.
type Body struct {
	ID64              int64   `json:"id64"`
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	SubType           string  `json:"subType"`
	DistanceToArrival float64 `json:"distanceToArrival"`
}
