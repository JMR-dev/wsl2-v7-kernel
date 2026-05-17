package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

var KEYWORDS = []string{
	`hv_`, `dxg`, `wsl`, `microsoft`, `hyperv`, `vmbus`, `mshv`, `dxgk`,
	`msft`, `9p`, `v9fs`, `azure`, `hyper-v`,
}

var CRITICAL_PATHS = []string{
	"Microsoft/", "drivers/hv/", "drivers/gpu/drm/hyperv/", "fs/9p/",
}

func main() {
	files, err := getConflictedFiles()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Resuming resolution for %d remaining conflicts...\n", len(files))

	var wg sync.WaitGroup
	// Concurrency limit for Git operations to prevent locking issues
	semaphore := make(chan struct{}, 10) 

	for _, file := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			
			if shouldAutoResolve(f) {
				semaphore <- struct{}{}
				resolveTheirs(f)
				<-semaphore
			}
		}(file)
	}

	wg.Wait()
	fmt.Println("Resume script execution complete.")
}

func getConflictedFiles() ([]string, error) {
	out, err := exec.Command("git", "diff", "--name-only", "--diff-filter=U").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	var filtered []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			filtered = append(filtered, l)
		}
	}
	return filtered, nil
}

func shouldAutoResolve(filepath string) bool {
	for _, p := range CRITICAL_PATHS {
		if strings.Contains(filepath, p) {
			return false
		}
	}

	content, err := os.ReadFile(filepath)
	if err != nil {
		return false
	}

	re := regexp.MustCompile(`(?s)<<<<<<< HEAD\n(.*?)\n=======\n(.*?)\n>>>>>>>`)
	matches := re.FindAllStringSubmatch(string(content), -1)

	if len(matches) == 0 {
		return false
	}

	for _, m := range matches {
		ours := m[1]
		for _, kw := range KEYWORDS {
			match, _ := regexp.MatchString("(?i)"+kw, ours)
			if match {
				return false
			}
		}
		if strings.Count(ours, "\n") > 10 {
			return false
		}
	}

	return true
}

func resolveTheirs(filepath string) {
	fmt.Printf("Auto-resolving: %s\n", filepath)
	exec.Command("git", "checkout", "--theirs", filepath).Run()
	exec.Command("git", "add", filepath).Run()
}
