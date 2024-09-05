package inmemoryprovider

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nbigot/minijob/job"
	"go.uber.org/zap"
)

type DBStorageFileStruct struct {
	Jobs job.JobMap `json:"jobs"`
}

type DBFileStorage struct {
	logger    *zap.Logger
	directory string // directory path to store file(s)
	filename  string // path to the file storing the data
}

func (s *DBFileStorage) Init() error {
	return nil
}

func (s *DBFileStorage) Load() (job.JobMap, error) {
	// load jobs from file
	// note: to prevent late error finding regarding directory and file creation,
	// ensure first that the directory exists or creation is possible.

	// if directory does not exist, create directory
	if _, err := os.Stat(s.directory); os.IsNotExist(err) {
		if err := os.MkdirAll(s.directory, 0755); err != nil {
			return nil, err
		}
	}

	// if file does not exist, create file
	if _, err := os.Stat(s.GetFilePath()); os.IsNotExist(err) {
		// create empty job map
		jobs := job.JobMap{}
		// save job map to file
		if err := s.Save(jobs); err != nil {
			return nil, err
		}
		return jobs, nil
	}

	// load jobs from file
	file, err := os.Open(s.GetFilePath())
	if err != nil {
		return nil, err
	}
	defer file.Close()

	jsonDecoder := json.NewDecoder(file)
	data := DBStorageFileStruct{}
	err = jsonDecoder.Decode(&data)
	if err != nil {
		s.logger.Error("Can't decode json stream",
			zap.String("topic", "stream"),
			zap.String("method", "LoadStreamFromUUID"),
			zap.String("filename", s.GetFilePath()), zap.Error(err),
		)
		return nil, err
	}

	return data.Jobs, nil
}

func (s *DBFileStorage) Save(jobs job.JobMap) error {
	// save file to disk
	fullfilepath := s.GetFilePath()
	obj := DBStorageFileStruct{Jobs: jobs}
	if sJson, err := json.Marshal(obj); err != nil {
		s.logger.Fatal(
			"Can't serialize json",
			zap.String("topic", "DBFileStorage"),
			zap.String("method", "Save"),
			zap.String("filename", fullfilepath),
			zap.Error(err),
		)
		return err
	} else {
		if err := os.WriteFile(fullfilepath, sJson, 0644); err != nil {
			s.logger.Fatal(
				"Can't save json to file",
				zap.String("topic", "DBFileStorage"),
				zap.String("method", "Save"),
				zap.String("filename", fullfilepath),
				zap.Error(err),
			)
			return err
		}
	}

	return nil
}

func (s *DBFileStorage) GetFilePath() string {
	return filepath.Join(s.directory, s.filename)
}

func NewDBFileStorage(logger *zap.Logger, directory string, filename string) *DBFileStorage {
	return &DBFileStorage{logger: logger, directory: directory, filename: filename}
}
