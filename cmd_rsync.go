package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runRsync implements "c4sh rsync" — smart rsync wrapper that understands
// c4m files. Transfers a c4m and its referenced store objects to/from a
// remote machine using rsync.
//
// Push: c4sh rsync project.c4m remote:/deliveries/
// Pull: c4sh rsync remote:/deliveries/project.c4m .
func runRsync(args []string) {
	if len(args) < 2 {
		die("rsync: usage: c4sh rsync <c4m> <remote:path>  (push)\n       c4sh rsync <remote:path/file.c4m> <local>  (pull)")
	}

	src := args[0]
	dst := args[1]

	// Extra rsync flags the user may pass (e.g. -v, --progress)
	extra := args[2:]

	// Detect push vs pull by which argument has a ":" (remote).
	srcRemote := isRemotePath(src)
	dstRemote := isRemotePath(dst)

	if srcRemote && dstRemote {
		die("rsync: both source and destination are remote — one must be local")
	}
	if !srcRemote && !dstRemote {
		die("rsync: neither source nor destination is remote — use c4sh cp instead")
	}

	if dstRemote {
		rsyncPush(src, dst, extra)
	} else {
		rsyncPull(src, dst, extra)
	}
}

// rsyncPush sends a local c4m and its store objects to a remote machine.
//
//  1. Parse local c4m to extract C4 IDs
//  2. rsync the entire local store to remote store with --ignore-existing
//     (rsync skips files with same name — same C4 ID = same content)
//  3. rsync the c4m file itself to the remote path
func rsyncPush(c4mPath, remoteDst string, extra []string) {
	// Resolve .c4m extension
	if !strings.HasSuffix(c4mPath, ".c4m") {
		if _, err := os.Stat(c4mPath + ".c4m"); err == nil {
			c4mPath += ".c4m"
		}
	}

	// Validate the c4m parses (and count referenced IDs for reporting)
	m, err := loadManifest(c4mPath)
	if err != nil {
		die("rsync: %v", err)
	}
	var idCount int
	for _, e := range m.Entries {
		if !e.C4ID.IsNil() {
			idCount++
		}
	}

	// Open local store
	store, err := openStore()
	if err != nil || store == nil {
		die("rsync: no local store configured (set C4_STORE)")
	}
	localStorePath := storeRoot(store)
	if localStorePath == "" {
		die("rsync: requires a local filesystem store (S3 stores cannot be rsynced)")
	}
	localStorePath = ensureTrailingSlash(localStorePath)

	// Remote store path
	remoteHost, _ := splitRemote(remoteDst)
	remoteStore := ensureTrailingSlash(remoteStorePath(remoteHost))

	// Step 1: sync store objects
	fmt.Fprintf(os.Stderr, "c4sh: rsync: pushing store (%d objects referenced) ...\n", idCount)
	storeArgs := []string{
		"-r",
		"--ignore-existing",
		localStorePath,
		remoteStore,
	}
	storeArgs = append(storeArgs, extra...)
	if err := runExternalRsync(storeArgs); err != nil {
		die("rsync: store transfer failed: %v", err)
	}

	// Step 2: sync the c4m file
	fmt.Fprintf(os.Stderr, "c4sh: rsync: pushing %s ...\n", c4mPath)
	fileArgs := []string{c4mPath, remoteDst}
	fileArgs = append(fileArgs, extra...)
	if err := runExternalRsync(fileArgs); err != nil {
		die("rsync: c4m transfer failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "c4sh: rsync: done\n")
}

// rsyncPull fetches a remote c4m and its store objects to the local machine.
//
//  1. rsync the c4m file from remote to local (small, fast)
//  2. Parse the local copy to extract C4 IDs
//  3. rsync the remote store to local store with --ignore-existing
//     (only transfers objects not already present locally)
func rsyncPull(remoteSrc, localDst string, extra []string) {
	// Step 1: pull the c4m file
	fmt.Fprintf(os.Stderr, "c4sh: rsync: pulling c4m ...\n")
	fileArgs := []string{remoteSrc, localDst}
	fileArgs = append(fileArgs, extra...)
	if err := runExternalRsync(fileArgs); err != nil {
		die("rsync: c4m transfer failed: %v", err)
	}

	// Determine local c4m path after rsync
	_, remotePath := splitRemote(remoteSrc)
	localC4m := localDst
	info, err := os.Stat(localDst)
	if err == nil && info.IsDir() {
		// rsync put the file inside the directory
		base := remotePath
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		localC4m = strings.TrimSuffix(localDst, "/") + "/" + base
	}

	// Step 2: parse the c4m
	m, err := loadManifest(localC4m)
	if err != nil {
		die("rsync: pulled c4m but cannot parse: %v", err)
	}
	var idCount int
	for _, e := range m.Entries {
		if !e.C4ID.IsNil() {
			idCount++
		}
	}

	// Open local store
	store, err := openStore()
	if err != nil || store == nil {
		die("rsync: no local store configured (set C4_STORE)")
	}
	localStorePath := storeRoot(store)
	if localStorePath == "" {
		die("rsync: requires a local filesystem store (S3 stores cannot be rsynced)")
	}
	localStorePath = ensureTrailingSlash(localStorePath)

	// Remote store path
	remoteHost, _ := splitRemote(remoteSrc)
	remoteStore := ensureTrailingSlash(remoteStorePath(remoteHost))

	// Step 3: sync store objects
	fmt.Fprintf(os.Stderr, "c4sh: rsync: pulling store (%d objects referenced) ...\n", idCount)
	storeArgs := []string{
		"-r",
		"--ignore-existing",
		remoteStore,
		localStorePath,
	}
	storeArgs = append(storeArgs, extra...)
	if err := runExternalRsync(storeArgs); err != nil {
		die("rsync: store transfer failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "c4sh: rsync: done\n")
}

// runExternalRsync invokes rsync as a subprocess, inheriting stdio.
func runExternalRsync(args []string) error {
	cmd := exec.Command("rsync", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isRemotePath returns true if the path looks like a remote rsync path
// (contains ":" but is not an absolute Windows path or c4m internal path).
func isRemotePath(p string) bool {
	// host:path or user@host:path
	i := strings.Index(p, ":")
	if i < 0 {
		return false
	}
	// Exclude c4m internal paths like "project.c4m:subdir/"
	prefix := p[:i]
	if strings.HasSuffix(prefix, ".c4m") {
		return false
	}
	// Exclude absolute paths like C:\... (Windows)
	if len(prefix) == 1 && prefix[0] >= 'A' && prefix[0] <= 'Z' {
		return false
	}
	return true
}

// splitRemote splits "host:/path" into ("host:", "/path").
// For "user@host:/path" returns ("user@host:", "/path").
func splitRemote(p string) (hostColon string, path string) {
	i := strings.Index(p, ":")
	if i < 0 {
		return "", p
	}
	return p[:i+1], p[i+1:]
}

// remoteStorePath returns the rsync remote path for the store on a given host.
// Uses C4_REMOTE_STORE env override, or defaults to ~/.c4/store on remote.
func remoteStorePath(hostColon string) string {
	if override := os.Getenv("C4_REMOTE_STORE"); override != "" {
		return hostColon + override
	}
	return hostColon + ".c4/store"
}

// ensureTrailingSlash ensures a path ends with "/" (important for rsync
// directory semantics — trailing slash means "contents of").
func ensureTrailingSlash(p string) string {
	if !strings.HasSuffix(p, "/") {
		return p + "/"
	}
	return p
}
