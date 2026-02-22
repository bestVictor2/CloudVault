package service

import (
	"CloudVault/internal/repo"
	"CloudVault/model"
	"fmt"
	"path"
	"strings"
)

type ArchiveEntry struct {
	ZipPath string
	FileObj *model.FileObject
	IsDir   bool
}

func sanitizeArchiveName(name string) string {
	clean := strings.TrimSpace(name)
	clean = strings.ReplaceAll(clean, "\\", "/")
	clean = strings.ReplaceAll(clean, "/", "_")
	clean = strings.ReplaceAll(clean, "..", "_")
	if clean == "" || clean == "." {
		return "unnamed"
	}
	return clean
}

// BuildArchiveEntries collects files and folders for zip download.
func BuildArchiveEntries(userID uint64, fileIDs []uint64) ([]ArchiveEntry, error) {
	entries := make([]ArchiveEntry, 0)
	for _, fileID := range fileIDs {
		var file model.UserFile
		if err := repo.Db.
			Where("id = ? AND user_id = ? AND is_deleted = 0", fileID, userID).
			First(&file).Error; err != nil {
			return nil, err
		}
		if file.IsDir {
			dirPath := sanitizeArchiveName(file.Name)
			entries = append(entries, ArchiveEntry{
				ZipPath: dirPath + "/",
				IsDir:   true,
			})
			if err := collectArchiveChildren(userID, file.ID, dirPath, &entries); err != nil {
				return nil, err
			}
			continue
		}
		if file.ObjectID == nil {
			return nil, fmt.Errorf("file %d has no object", file.ID)
		}
		obj, err := GetFileObjectById(*file.ObjectID)
		if err != nil {
			return nil, err
		}
		entries = append(entries, ArchiveEntry{
			ZipPath: sanitizeArchiveName(file.Name),
			FileObj: obj,
			IsDir:   false,
		})
	}
	return entries, nil
}

func collectArchiveChildren(userID, parentID uint64, prefix string, entries *[]ArchiveEntry) error {
	var children []model.UserFile
	if err := repo.Db.
		Where("parent_id = ? AND user_id = ? AND is_deleted = 0", parentID, userID).
		Find(&children).Error; err != nil {
		return err
	}
	for _, child := range children {
		childPath := path.Join(prefix, sanitizeArchiveName(child.Name))
		if child.IsDir {
			*entries = append(*entries, ArchiveEntry{
				ZipPath: childPath + "/",
				IsDir:   true,
			})
			if err := collectArchiveChildren(userID, child.ID, childPath, entries); err != nil {
				return err
			}
			continue
		}
		if child.ObjectID == nil {
			return fmt.Errorf("file %d has no object", child.ID)
		}
		obj, err := GetFileObjectById(*child.ObjectID)
		if err != nil {
			return err
		}
		*entries = append(*entries, ArchiveEntry{
			ZipPath: childPath,
			FileObj: obj,
			IsDir:   false,
		})
	}
	return nil
}



