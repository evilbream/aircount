package tplink

import (
	"testing"

	"github.com/rs/zerolog/log"
)

func addD(lister *[]int) {
	for i := range 10 {
		*lister = append(*lister, i)
	}
}

func TestAddDev(t *testing.T) {
	addition := make([]int, 0)
	addD(&addition)
	addD(&addition)
	log.Info().Any("list", addition)
}
