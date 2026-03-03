package drive

import "github.com/H-strangeone/lan-suite/internal/config"

type Drive struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Drive {
	return &Drive{cfg: cfg}
}
