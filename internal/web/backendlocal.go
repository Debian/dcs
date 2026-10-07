package web

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Debian/dcs/internal/index"
	"github.com/Debian/dcs/internal/ranking"
	"github.com/Debian/dcs/internal/sourcebackend"
	"github.com/Debian/dcs/internal/web/common"
)

func (o *Opts) openSourceIndexes() ([]common.Backend, error) {
	rankingMap, err := ranking.ReadRankingData(o.RankingDataPath)
	if err != nil {
		return nil, err
	}

	var backends []common.Backend
	for _, dir := range strings.Split(o.SourceIndexes, ",") {
		if strings.TrimSpace(dir) == "" {
			continue
		}

		indexPath := filepath.Join(dir, "full")
		openPath := indexPath

		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			tmp, err := os.MkdirTemp("", "dcs-index-backend")
			if err != nil {
				return nil, err
			}
			defer os.Remove(tmp)
			openPath = tmp
			ix, err := index.Create(openPath)
			if err != nil {
				return nil, err
			}
			if err := ix.Flush(); err != nil {
				return nil, err
			}
		}

		ix, err := index.Open(openPath)
		if err != nil {
			return nil, err
		}

		unpacked, err := os.OpenRoot(filepath.Join(dir, "src"))
		if err != nil {
			return nil, err
		}

		srv := &sourcebackend.Server{
			Index:              ix,
			UnpackedPath:       unpacked,
			IndexPath:          indexPath,
			UsePositionalIndex: o.UsePositionalIndex,
			RankingMap:         rankingMap,
		}
		go watchIndex(srv)
		backends = append(backends, srv)
	}
	return backends, nil
}

func watchIndex(srv *sourcebackend.Server) {
	current, _ := filepath.EvalSymlinks(srv.IndexPath)
	// an error in filepath.EvalSymlinks will result in one extra ReplaceIndex
	for range time.Tick(1 * time.Second) {
		target, err := filepath.EvalSymlinks(srv.IndexPath)
		if err != nil || target == current {
			continue
		}
		newShard := filepath.Base(target)
		log.Printf("Trying to load %q\n", newShard)
		if err := srv.ReplaceIndex(newShard); err != nil {
			log.Printf("ReplaceIndex(%s): %v", newShard, err)
			continue
		}
		current = target
	}
}
