package file

import "github.com/H-strangeone/lan-suite/internal/config"

const ChunkSize = 256 * 1024

type Chunker struct {
	cfg *config.Config
}

func NewChunker(cfg *config.Config) *Chunker {
	return &Chunker{cfg: cfg}
}
