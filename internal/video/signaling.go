package video

import "github.com/H-strangeone/lan-suite/internal/config"

type Signaler struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Signaler {
	return &Signaler{cfg: cfg}
}
