package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// KEYWORDS used to identify custom WSL logic in conflict blocks
var KEYWORDS = []string{
	`hv_`, `dxg`, `wsl`, `microsoft`, `hyperv`, `vmbus`, `mshv`, `dxgk`,
	`msft`, `9p`, `v9fs`, `azure`, `hyper-v`,
}

// Critical paths that should always be manually reviewed
var CRITICAL_PATHS = []string{
	"Microsoft/", "drivers/hv/", "drivers/gpu/drm/hyperv/", "fs/9p/",
}

type Result struct {
	File   string
	Status string
	Reason string
}

func main() {
	files, err := getConflictedFiles()
	if err != nil {
		fmt.Printf("Error getting conflicts: %v\n", err)
		return
	}

	numFiles := len(files)
	fmt.Printf("Analyzing %d files using %d workers...\n", numFiles, runtime.NumCPU())

	results := make(chan Result, numFiles)
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, runtime.NumCPU()*2)

	for _, file := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			semaphore <- struct{}{}
			results <- analyzeFile(f)
			<-semaphore
		}(file)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	autoFiles := []string{}
	manualFiles := []Result{}

	for res := range results {
		if res.Status == "AUTO_THEIRS" {
			autoFiles = append(autoFiles)
		} else {
			manualFiles = append(manualFiles, res)
		}
	}

	// Generate resolution script
	writeAutoScript("resolve_auto.sh", autoFiles)
	writeManualLog("manual_triage.txt", manualFiles)

	fmt.Printf("\nDone.\nAuto-resolvable: %d\nRequire manual review: %d\n", len(autoFiles), len(manualFiles))
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

func analyzeFile(filepath string) Result {
	// Path check
	for _, p := range CRITICAL_PATHS {
		if strings.Contains(filepath, p) {
			return Result{filepath, "MANUAL", "Critical path"}
		}
	}

	content, err := os.ReadFile(filepath)
	if err != nil {
		return Result{filepath, "ERROR", err.Error()}
	}

	// Simple regex for conflict markers
	re := regexp.MustCompile(`(?s)<<<<<<< HEAD\n(.*?)\n=======\n(.*?)\n>>>>>>>`)
	matches := re.FindAllStringSubmatch(string(content), -1)

	if len(matches) == 0 {
		return Result{filepath, "NO_MARKERS", ""}
	}

	for _, m := range matches {
		ours := m[1]
		
		// Keyword check
		for _, kw := range KEYWORDS {
			match, _ := regexp.MatchString("(?i)"+kw, ours)
			if match {
				return Result{filepath, "MANUAL", fmt.Sprintf("Keyword match: %s", kw)}
			}
		}

		// Length check
		lines := strings.Count(ours, "\n")
		if lines > 10 {
			return Result{filepath, "MANUAL", "WSL block > 10 lines"}
		}
	}

	return Result{filepath, "AUTO_THEIRS", ""}
}

func writeAutoScript(filename string, files []string) {
	f, _ := os.Create(filename)
	defer f.Close()
	w := bufio.NewWriter(f)
	w.WriteString("#!/bin/bash\n# Automatically generated parallel resolver\n")
	for _, file := range files {
		w.WriteString(fmt.Sprintf("git checkout --theirs '%s' && git add '%s' &\n", file, file))
		w.WriteString("if (( $(jobs -r | wc -l) >= 20 )); then wait -n; fi\n")
	}
	w.WriteString("wait\necho 'Auto-resolution complete.'\n")
	w.Flush()
}

func writeManualLog(filename string, results []Result) {
	f, _ := os.Create(filename)
	defer f.Close()
	for _, res := range results {
		f.WriteString(fmt.Sprintf("%s [%s]: %s\n", res.File, res.Status, res.Reason))
	}
}
