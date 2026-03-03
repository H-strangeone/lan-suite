package file

import "github.com/H-strangeone/lan-suite/internal/config"

type Transfer struct {
	cfg *config.Config
}

func NewTransfer(cfg *config.Config) *Transfer {
	return &Transfer{cfg: cfg}
}
