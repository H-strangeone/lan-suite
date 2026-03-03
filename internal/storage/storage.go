package storage

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/H-strangeone/lan-suite/internal/config"
)

const encExt = ".enc"

type Store struct {
	cfg     *config.Config
	dataDir string
	cipher  *StorageCipher

	mu     sync.Mutex
	seqnos map[string]uint64
}

func New(cfg *config.Config, privKey ed25519.PrivateKey) (*Store, error) {
	dirs := []string{
		cfg.DataDir,
		filepath.Join(cfg.DataDir, "chat"),
		filepath.Join(cfg.DataDir, "drive"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	sc, err := NewStorageCipher([]byte(privKey[:32]))
	if err != nil {
		return nil, fmt.Errorf("initialising storage cipher: %w", err)
	}

	s := &Store{
		cfg:     cfg,
		dataDir: cfg.DataDir,
		cipher:  sc,
		seqnos:  make(map[string]uint64),
	}
	return s, nil
}

type StoredMessage struct {
	SeqNo      uint64 `json:"seq_no"`
	ID         string `json:"id"`
	Room       string `json:"room"`
	FromNodeID string `json:"from_node_id"`
	FromName   string `json:"from_name"`
	Text       string `json:"text"`
	Timestamp  string `json:"timestamp"`
	ReplyTo    string `json:"reply_to,omitempty"`
}

func (s *Store) SaveMessage(msg *StoredMessage) (uint64, error) {
	s.mu.Lock()
	seqno := s.nextSeqno(msg.Room)
	msg.SeqNo = seqno
	s.mu.Unlock()

	roomDir := filepath.Join(s.dataDir, "chat", sanitizeRoomID(msg.Room))
	if err := os.MkdirAll(roomDir, 0700); err != nil {
		return 0, fmt.Errorf("creating room dir: %w", err)
	}

	plaintext, err := json.Marshal(msg)
	if err != nil {
		return 0, fmt.Errorf("marshaling message: %w", err)
	}

	ciphertext, err := s.cipher.Encrypt(plaintext)
	if err != nil {
		return 0, fmt.Errorf("encrypting message: %w", err)
	}

	filename := fmt.Sprintf("%010d%s", seqno, encExt)
	path := filepath.Join(roomDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("writing message file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(ciphertext); err != nil {
		return 0, fmt.Errorf("writing ciphertext: %w", err)
	}

	return seqno, nil
}

func (s *Store) LoadMessages(roomID string, afterSeqno uint64, limit int) ([]*StoredMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	roomDir := filepath.Join(s.dataDir, "chat", sanitizeRoomID(roomID))
	if _, err := os.Stat(roomDir); os.IsNotExist(err) {
		return []*StoredMessage{}, nil
	}

	entries, err := os.ReadDir(roomDir)
	if err != nil {
		return nil, fmt.Errorf("reading room dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var messages []*StoredMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), encExt) {
			continue
		}

		seqStr := strings.TrimSuffix(entry.Name(), encExt)
		seqno, err := strconv.ParseUint(seqStr, 10, 64)
		if err != nil {
			continue
		}
		if seqno <= afterSeqno {
			continue
		}

		data, err := os.ReadFile(filepath.Join(roomDir, entry.Name()))
		if err != nil {
			continue
		}

		plaintext, err := s.cipher.Decrypt(data)
		if err != nil {

			fmt.Printf("[storage] decrypt error seq=%d room=%s: %v\n", seqno, roomID, err)
			continue
		}

		var msg StoredMessage
		if err := json.Unmarshal(plaintext, &msg); err != nil {
			continue
		}

		messages = append(messages, &msg)
		if len(messages) >= limit {
			break
		}
	}

	if messages == nil {
		messages = []*StoredMessage{}
	}
	return messages, nil
}

func (s *Store) LatestSeqno(roomID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seqnos[roomID]
}

func (s *Store) LoadRoomSeqnos() error {
	chatDir := filepath.Join(s.dataDir, "chat")
	rooms, err := os.ReadDir(chatDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading chat dir: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, room := range rooms {
		if !room.IsDir() {
			continue
		}
		roomDir := filepath.Join(chatDir, room.Name())
		entries, err := os.ReadDir(roomDir)
		if err != nil {
			continue
		}
		var max uint64
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), encExt) {
				continue
			}
			seqStr := strings.TrimSuffix(entry.Name(), encExt)
			seqno, err := strconv.ParseUint(seqStr, 10, 64)
			if err != nil {
				continue
			}
			if seqno > max {
				max = seqno
			}
		}
		s.seqnos[room.Name()] = max
	}
	return nil
}

func (s *Store) nextSeqno(roomID string) uint64 {
	s.seqnos[roomID]++
	return s.seqnos[roomID]
}

func sanitizeRoomID(roomID string) string {
	var b strings.Builder
	for _, r := range roomID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "default"
	}
	return result
}
