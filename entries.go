// entries.go
package main

import (
	"mime"
	"os"
	"path"
)

// For directory entry sorting:

type Entries []os.FileInfo

func (s Entries) Len() int      { return len(s) }
func (s Entries) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type sortBy int

const (
	sortByName sortBy = iota
	sortByDate
	sortBySize
)

type sortDirection int

const (
	sortAscending sortDirection = iota
	sortDescending
)

// Sort by name:
type ByName struct {
	Entries
	dir sortDirection
}

func (s ByName) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].Name() < s.Entries[j].Name()
	} else {
		return s.Entries[i].Name() > s.Entries[j].Name()
	}
}

// Sort by last modified time:
type ByDate struct {
	Entries
	dir sortDirection
}

func (s ByDate) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].ModTime().Before(s.Entries[j].ModTime())
	} else {
		return s.Entries[i].ModTime().After(s.Entries[j].ModTime())
	}
}

// Sort by size:
type BySize struct {
	Entries
	dir sortDirection
}

func (s BySize) Less(i, j int) bool {
	if s.Entries[i].IsDir() && !s.Entries[j].IsDir() {
		return true
	}
	if !s.Entries[i].IsDir() && s.Entries[j].IsDir() {
		return false
	}

	if s.dir == sortAscending {
		return s.Entries[i].Size() < s.Entries[j].Size()
	} else {
		return s.Entries[i].Size() > s.Entries[j].Size()
	}
}

func followSymlink(localPath string, dfi os.FileInfo) os.FileInfo {
	// Check symlink:
	if (dfi.Mode() & os.ModeSymlink) != 0 {

		dfiPath := path.Join(localPath, dfi.Name())
		if targetPath, err := os.Readlink(dfiPath); err == nil {
			// Find the absolute path of the symlink's target:
			if !path.IsAbs(targetPath) {
				targetPath = path.Join(localPath, targetPath)
			}
			if tdfi, err := os.Stat(targetPath); err == nil {
				// Change to the target so we get its properties instead of the symlink's:
				return tdfi
			}
		}
	}

	return dfi
}

var _audio_mimetypes = map[string]bool{
	"audio/mpeg":  true,
	"audio/ogg":   true,
	"audio/wav":   true,
	"audio/x-wav": true,
}

var _audio_extensions = map[string]bool{
	".mp3": true,
	".ogg": true,
}

func isMP3(filename string) bool {
	ext := path.Ext(filename)
	if _, ok := _audio_extensions[ext]; ok {
		return true
	}

	mt := mime.TypeByExtension(ext)
	// log.Printf("'%s': '%s'\n", ext, mt)
	if _, ok := _audio_mimetypes[mt]; ok {
		return true
	}

	return false
}

func isMultitrack(filename string) bool {
	ext := path.Ext(filename)
	if ext == ".opus" {
		return true
	}

	return false
}
