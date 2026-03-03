package compress

type Level int

const (
	LevelFastest Level = 1
	LevelDefault Level = 3
	LevelBest    Level = 7
)

var Magic = [4]byte{'L', 'S', 'Z', 'S'}

var AlreadyCompressedMIME = map[string]bool{
	"image/jpeg":                  true,
	"image/png":                   true,
	"image/gif":                   true,
	"image/webp":                  true,
	"video/mp4":                   true,
	"video/webm":                  true,
	"video/x-matroska":            true,
	"audio/mpeg":                  true,
	"audio/ogg":                   true,
	"application/zip":             true,
	"application/gzip":            true,
	"application/x-7z-compressed": true,
}

func ShouldCompress(filename string, header []byte) bool {

	return true
}
