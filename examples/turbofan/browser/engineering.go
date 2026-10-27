//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"

	"m31labs.dev/gosx/examples/turbofan/cycle"
)

type engineeringBinding struct {
	node              js.Value
	key, format, attr string
	scale             float64
	digits            int
}
type sweepBinding struct {
	node    js.Value
	key     string
	maximum float64
	second  bool
}
type engineeringBindings struct {
	fields  []engineeringBinding
	markers []sweepBinding
}

func bindEngineering(root js.Value) engineeringBindings {
	b := engineeringBindings{}
	fields := root.Call("querySelectorAll", "[data-eng-key]")
	for i := 0; i < fields.Length(); i++ {
		node := fields.Index(i)
		get := func(k string) string {
			v := node.Call("getAttribute", k)
			if v.IsNull() {
				return ""
			}
			return v.String()
		}
		key := get("data-eng-key")
		if key == "" {
			continue
		}
		scale, err := strconv.ParseFloat(get("data-eng-scale"), 64)
		if err != nil {
			scale = 1
		}
		digits, err := strconv.Atoi(get("data-eng-digits"))
		if err != nil {
			digits = 2
		}
		b.fields = append(b.fields, engineeringBinding{node, key, get("data-eng-format"), get("data-eng-attr"), scale, digits})
	}
	markers := root.Call("querySelectorAll", "[data-sweep-marker],[data-sweep-second]")
	for i := 0; i < markers.Length(); i++ {
		node := markers.Index(i)
		second := node.Call("hasAttribute", "data-sweep-second").Bool()
		attribute := "data-sweep-marker"
		if second {
			attribute = "data-sweep-second"
		}
		maximum, _ := strconv.ParseFloat(node.Call("getAttribute", "data-sweep-max").String(), 64)
		b.markers = append(b.markers, sweepBinding{node, node.Call("getAttribute", attribute).String(), maximum, second})
	}
	return b
}

func (b engineeringBindings) update(d cycle.EngineeringRecord, mapped *cycle.MappedResult, model *cycle.MappedModel, valid bool) {
	for _, field := range b.fields {
		value := "—"
		if field.format == "t" {
			if text, ok := d.Text[field.key]; ok {
				value = text
			}
		} else if x, ok := d.Numbers[field.key]; ok {
			format := byte('f')
			if field.format == "e" {
				format = 'e'
			}
			value = strconv.FormatFloat(x*field.scale, format, field.digits, 64)
		}
		if field.attr != "" {
			if value == "—" {
				value = ""
			}
			field.node.Call("setAttribute", field.attr, value)
		} else {
			field.node.Set("textContent", value)
		}
	}
	for _, marker := range b.markers {
		if !valid || mapped == nil || marker.second && marker.key != "speed" {
			marker.node.Call("setAttribute", "visibility", "hidden")
			continue
		}
		p := cycle.SweepPoint{Fuel: mapped.Command.FuelFlowKGS, Thrust: mapped.Cycle.NetThrustN, N1: mapped.N1RPM, N2: mapped.N2RPM, OPR: mapped.Cycle.Inputs.OverallPressureRatio, TSFC: mapped.Cycle.TSFC}
		value, other := cycle.SweepValues(p, marker.key, model)
		if marker.second {
			value = other
		}
		x, y := cycle.SweepPosition(p.Fuel, value, marker.maximum)
		marker.node.Call("setAttribute", "cx", strconv.FormatFloat(x, 'f', 2, 64))
		marker.node.Call("setAttribute", "cy", strconv.FormatFloat(y, 'f', 2, 64))
		marker.node.Call("setAttribute", "visibility", "visible")
	}
}
