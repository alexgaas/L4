package util

import (
	"math/rand"
	"time"
)

func NewSeed(seed *[]byte) {
	rand.Seed(time.Now().UnixNano())
	*seed = make([]byte, 8)
	rand.Read(*seed)
}
