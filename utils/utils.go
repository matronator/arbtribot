package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
)

func ReadLines(path string) ([]string, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    var lines []string
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        lines = append(lines, scanner.Text())
    }
    return lines, scanner.Err()
}

func GetRoot() string {
	_, b, _, _ := runtime.Caller(0)
	return filepath.Dir(b+"/../../")
}
