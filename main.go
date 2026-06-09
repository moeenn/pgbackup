package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	LOG_FILE        = "log.txt"
	RETENTION_WEEKS = 2
	SLEEP_HOURS     = 12
)

type config struct {
	PgUser         string
	PgDatabase     string
	ContainerName  string
	RetentionWeeks int
}

func readEnv(key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return val, fmt.Errorf("missing env var: %s", key)
	}
	return strings.TrimSpace(val), nil
}

func newConfig() (*config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("failed to load env: %w", err)
	}

	user, err := readEnv("PG_USER")
	if err != nil {
		return nil, err
	}

	db, err := readEnv("PG_DB")
	if err != nil {
		return nil, err
	}

	containerName, err := readEnv("CONTAINER_NAME")
	if err != nil {
		return nil, err
	}

	cfg := config{
		PgUser:         user,
		PgDatabase:     db,
		ContainerName:  containerName,
		RetentionWeeks: RETENTION_WEEKS,
	}

	return &cfg, nil
}

func now() string {
	currentTime := time.Now()
	return currentTime.Format(time.DateTime)
}

func appendToFile(filePath string, data string) error {
	f, err := getLogFile()
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(data); err != nil {
		return fmt.Errorf("failed to write to file (%s): %w", filePath, err)
	}
	return nil
}

func getLogFile() (*os.File, error) {
	filepath := path.Join(".", LOG_FILE)
	f, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	return f, nil
}

func logToFile(level, message string) {
	logFile := path.Join(".", LOG_FILE)
	data := fmt.Sprintf("%s %s: %s\n", now(), level, message)

	if err := appendToFile(logFile, data); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write to log file: %s", err.Error())
	}

	fmt.Printf("%s %s: %s", now(), level, message)
}

func exportDatabase(c *config, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file (%s): %w", filename, err)
	}
	defer file.Close()

	logFile, err := getLogFile()
	if err != nil {
		return err
	}

	cmd := exec.Command("docker",
		"container",
		"exec",
		c.ContainerName,
		"/usr/local/bin/pg_dump",
		"-U",
		c.PgUser,
		c.PgDatabase)

	cmd.Stdout = file
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to export database: %w", err)
	}

	return nil
}

func cleanOldBackups(backupDir string, retentionWeeks int) error {
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("failed to list backup dir content: %w", err)
	}

	twoWeeksAgo := time.Now().AddDate(0, 0, -14)
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		fileInfo, err := file.Info()
		if err != nil {
			continue
		}

		if fileInfo.ModTime().Before(twoWeeksAgo) {
			fullpath := path.Join(backupDir, fileInfo.Name())
			if err := os.Remove(fullpath); err != nil {
				return fmt.Errorf("failed to remove backup file (%s): %w", file.Name(), err)
			}
		}
	}

	return nil
}

func run() error {
	cfg, err := newConfig()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	backupDir := path.Join(".", "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backups dir: %w", err)
	}

	filepath := path.Join(backupDir, now()+".sql")
	if err := exportDatabase(cfg, filepath); err != nil {
		logToFile("ERROR", err.Error())
		if err := os.Remove(filepath); err != nil {
			if !os.IsNotExist(err) {
				logToFile("ERROR", fmt.Sprintf("failed to remove failed export output file: %s", err.Error()))
			}
		}
	}

	if err := cleanOldBackups(backupDir, cfg.RetentionWeeks); err != nil {
		return fmt.Errorf("failed to clean-up old backups: %w", err)
	}

	logToFile("INFO", "backup job complete")
	return nil
}

func main() {
	sleepDuration := time.Hour * SLEEP_HOURS
	for {
		if err := run(); err != nil {
			logToFile("ERROR", fmt.Sprintf("unknown error: %s", err.Error()))
		}
		time.Sleep(sleepDuration)
	}
}
