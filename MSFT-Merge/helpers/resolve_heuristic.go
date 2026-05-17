package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var wslKeywords = []string{
	"wsl", "msft", "mshv", "hyperv", "dxg", "vmbus", "azure", "hyper-v",
}

func main() {
	// 1. Get the list of all unmerged/conflicted files.
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to get conflicted files: %v", err)
	}

	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(files) == 1 && files[0] == "" {
		fmt.Println("No conflicts found.")
		return
	}

	fmt.Printf("Found %d conflicted files. Applying parallel heuristic resolution...\n", len(files))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 40) // Process files in parallel
	
	var resolvedFiles []string
	var mu sync.Mutex

	for _, file := range files {
		if file == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(f string) {
			defer wg.Done()
			defer func() { <-sem }()
			
			if resolveFile(f) {
				mu.Lock()
				resolvedFiles = append(resolvedFiles, f)
				mu.Unlock()
			}
		}(file)
	}

	wg.Wait()
	
	fmt.Printf("\nProcessing complete. Staging %d files...\n", len(resolvedFiles))
	
	// 2. Stage files in batches of 100 to avoid command line length limits
	batchSize := 100
	for i := 0; i < len(resolvedFiles); i += batchSize {
		end := i + batchSize
		if end > len(resolvedFiles) {
			end = len(resolvedFiles)
		}
		
		batch := resolvedFiles[i:end]
		args := append([]string{"add"}, batch...)
		cmd := exec.Command("git", args...)
		if err := cmd.Run(); err != nil {
			log.Printf("Error staging batch starting at %d: %v", i, err)
		} else {
			fmt.Print(".")
		}
	}

	fmt.Println("\nHeuristic resolution complete. Please run 'git diff --staged' and 'make' to verify the results.")
}

func resolveFile(path string) bool {
	// 1. Check if the file has conflict markers.
	content, err := os.ReadFile(path)
	if err != nil {
		// If the file is missing from the worktree, it's likely a delete conflict.
		// We decide based on path.
		if isCriticalPath(path) {
			// If it's a critical WSL file but deleted, try to restore from WSL side.
			exec.Command("git", "checkout", "--theirs", path).Run()
		} else {
			// Otherwise accept the deletion (usually from upstream or old WSL).
			exec.Command("git", "rm", path).Run()
		}
		return true
	}

	lines := strings.Split(string(content), "\n")
	hasMarkers := false
	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<< HEAD") {
			hasMarkers = true
			break
		}
	}

	if !hasMarkers {
		// If no markers, use path heuristic to pick a side.
		if isCriticalPath(path) {
			exec.Command("git", "checkout", "--theirs", path).Run()
		} else {
			exec.Command("git", "checkout", "--ours", path).Run()
		}
		return true
	}

	// 2. Resolve using keyword heuristic if markers exist.
	var resolvedLines []string
	inConflict := false
	var headBlock []string // upstream v7.0.8 (HEAD)
	var wslBlock []string  // Microsoft/WSL branch (MERGE_HEAD)
	var currentBlock int   // 0 = not in conflict, 1 = HEAD, 2 = WSL

	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<< HEAD") {
			inConflict = true
			currentBlock = 1
			headBlock = nil
			wslBlock = nil
			continue
		} else if strings.HasPrefix(line, "=======") && inConflict {
			currentBlock = 2
			continue
		} else if strings.HasPrefix(line, ">>>>>>>") && inConflict {
			inConflict = false
			currentBlock = 0

			wslText := strings.Join(wslBlock, "\n")
			hasKeyword := false
			lowerWsl := strings.ToLower(wslText)
			
			if isCriticalPath(path) {
				hasKeyword = true
			} else {
				for _, kw := range wslKeywords {
					if strings.Contains(lowerWsl, kw) {
						hasKeyword = true
						break
					}
				}
			}

			if hasKeyword {
				resolvedLines = append(resolvedLines, wslBlock...)
			} else {
				resolvedLines = append(resolvedLines, headBlock...)
			}
			continue
		}

		if currentBlock == 0 {
			resolvedLines = append(resolvedLines, line)
		} else if currentBlock == 1 {
			headBlock = append(headBlock, line)
		} else if currentBlock == 2 {
			wslBlock = append(wslBlock, line)
		}
	}

	err = os.WriteFile(path, []byte(strings.Join(resolvedLines, "\n")), 0644)
	if err != nil {
		log.Printf("Error writing %s: %v", path, err)
		return false
	}

	return true
}

func isCriticalPath(path string) bool {
	return strings.HasPrefix(path, "Microsoft/") ||
		strings.HasPrefix(path, "drivers/hv/") ||
		strings.HasPrefix(path, "drivers/gpu/drm/dxg") ||
		strings.HasPrefix(path, "fs/9p/")
}
