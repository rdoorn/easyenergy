package main

import (
	"testing"
)

func TestGet(t *testing.T) {

	h := Handler{
		//statsd:     statsdhelper.New(),
		easyEnergy: &EasyEnergy{},
	}

	h.get()
	h.put()

}
