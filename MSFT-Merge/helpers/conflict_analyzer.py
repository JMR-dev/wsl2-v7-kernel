import os
import subprocess
import re

KEYWORDS = [
    r'hv_', r'dxg', r'wsl', r'microsoft', r'hyperv', r'vmbus', r'mshv', r'dxgk',
    r'msft', r'9p', r'v9fs', r'azure', r'hyper-v'
]

def get_conflicted_files():
    cmd = ['git', 'diff', '--name-only', '--diff-filter=U']
    result = subprocess.run(cmd, capture_output=True, text=True)
    return result.stdout.splitlines()

def analyze_file(filepath):
    if not os.path.exists(filepath):
        return "DELETED", []

    with open(filepath, 'r', errors='ignore') as f:
        content = f.read()

    conflicts = re.findall(r'<<<<<<< HEAD\n(.*?)\n=======\n(.*?)\n>>>>>>>', content, re.DOTALL)
    
    if not conflicts:
        return "NO_MARKERS", []

    reasons_for_manual = []
    
    # Path-based rules
    if any(p in filepath for p in ['Microsoft/', 'drivers/hv/', 'drivers/gpu/drm/hyperv/', 'fs/9p/']):
        reasons_for_manual.append(f"Critical WSL path: {filepath}")

    for ours, theirs in conflicts:
        # Keyword check in "ours"
        for kw in KEYWORDS:
            if re.search(kw, ours, re.IGNORECASE):
                reasons_for_manual.append(f"Keyword '{kw}' found in WSL block")
                break
        
        # Heuristic: if WSL block is significantly different from what we'd expect for simple context drift
        if len(ours.splitlines()) > 10:
             reasons_for_manual.append("WSL block > 10 lines")

    if reasons_for_manual:
        return "MANUAL", list(set(reasons_for_manual))
    else:
        return "AUTO_THEIRS", []

def main():
    files = get_conflicted_files()
    auto_list = []
    manual_list = []

    print(f"Analyzing {len(files)} conflicted files...")

    for f in files:
        status, reasons = analyze_file(f)
        if status == "AUTO_THEIRS":
            auto_list.append(f)
        else:
            manual_list.append((f, status, reasons))

    print(f"\nSummary:")
    print(f"Files for automatic resolution (--theirs): {len(auto_list)}")
    print(f"Files for manual resolution: {len(manual_list)}")

    with open('auto_resolve.sh', 'w') as f:
        f.write("#!/bin/bash\n")
        for file in auto_list:
            f.write(f"git checkout --theirs '{file}'\n")
            f.write(f"git add '{file}'\n")
    
    with open('manual_resolve.txt', 'w') as f:
        for file, status, reasons in manual_list:
            f.write(f"{file} ({status}): {', '.join(reasons)}\n")

    print("\nScripts generated: auto_resolve.sh and manual_resolve.txt")

if __name__ == "__main__":
    main()
