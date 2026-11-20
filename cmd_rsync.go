package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Avalanche-io/c4"
	"github.com/Avalanche-io/c4/c4m"
	c4store "github.com/Avalanche-io/c4/store"
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
// rsyncPush sends a local c4m and the store objects it names to a remote
// machine. Only those objects travel: every file's content, and every
// listing and range ID list the c4m references, through the listings and
// lists themselves. Nothing else in the local store is sent.
//
// Each object lands where the remote's store looks for it. The remote's
// folders are listed first (folders only, no files), and each object is
// placed in the deepest folder of its trie path that exists there, as the
// store's own Put would place it: at the root of a flat store, under c4/
// or c4/XX/ in a split one. Nothing new is created in the remote's trie.
func rsyncPush(c4mPath, remoteDst string, extra []string) {
	if !strings.HasSuffix(c4mPath, ".c4m") {
		if _, err := os.Stat(c4mPath + ".c4m"); err == nil {
			c4mPath += ".c4m"
		}
	}
	m, err := loadManifest(c4mPath)
	if err != nil {
		die("rsync: %v", err)
	}
	local, paths := openLocalTreeStore()

	objs := referencedObjects(m, local)
	var missing []string
	stage, err := os.MkdirTemp("", "c4sh-rsync-")
	if err != nil {
		die("rsync: %v", err)
	}
	defer os.RemoveAll(stage)

	remoteHost, _ := splitRemote(remoteDst)
	remoteStore := ensureTrailingSlash(remoteStorePath(remoteHost))
	layout, err := listRemoteLayout(remoteStore, extra)
	if err != nil {
		die("rsync: %v", err)
	}

	// A remote with no store yet gets one: the staged objects sit under the
	// store's own path, sent to its first folder that exists (the remote
	// home, or / for an absolute path), so rsync makes every folder on the way.
	prefix, dest := "", remoteStore
	if !layout.exists {
		prefix, dest = newStoreDest(remoteHost, strings.TrimPrefix(remoteStorePath(remoteHost), remoteHost))
	}

	staged := 0
	for _, o := range objs {
		src, ok := paths.ContentPath(o.id)
		if !ok {
			if o.required {
				missing = append(missing, o.name+" "+o.id.String())
			}
			continue
		}
		dst := filepath.Join(stage, filepath.FromSlash(path.Join(prefix, layout.place(o.id))))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			die("rsync: %v", err)
		}
		if err := os.Symlink(src, dst); err != nil {
			die("rsync: %v", err)
		}
		staged++
	}

	fmt.Fprintf(os.Stderr, "c4sh: rsync: pushing the %d store objects %s names ...\n", staged, filepath.Base(c4mPath))
	sent := 0
	if staged > 0 {
		// -L sends each staged link's object as a file; --ignore-existing
		// skips an object the remote already holds at that place.
		storeArgs := append([]string{"-rL", "--ignore-existing", "--out-format=%n",
			ensureTrailingSlash(stage), dest}, extra...)
		sent, err = runRsyncCounting(storeArgs)
		if err != nil {
			die("rsync: store transfer failed: %v", err)
		}
	}
	fmt.Fprintf(os.Stderr, "c4sh: rsync: sent %d objects, %d already on the remote\n", sent, staged-sent)

	fmt.Fprintf(os.Stderr, "c4sh: rsync: pushing %s ...\n", c4mPath)
	fileArgs := append([]string{c4mPath, remoteDst}, extra...)
	if err := runExternalRsync(fileArgs); err != nil {
		die("rsync: c4m transfer failed: %v", err)
	}
	reportMissing(missing, "not in the local store")
	fmt.Fprintf(os.Stderr, "c4sh: rsync: done\n")
	if len(missing) > 0 {
		osExit(1)
	}
}

// rsyncPull fetches a remote c4m and the store objects it names. It asks
// the remote for those objects only, by their place in the remote's trie,
// and puts each into the local store through the store itself, which
// checks that the bytes have the C4 ID their name says. A listing or range
// ID list it fetches may name more objects; those are fetched next, until
// nothing new is named.
func rsyncPull(remoteSrc, localDst string, extra []string) {
	fmt.Fprintf(os.Stderr, "c4sh: rsync: pulling c4m ...\n")
	fileArgs := append([]string{remoteSrc, localDst}, extra...)
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
	m, err := loadManifest(localC4m)
	if err != nil {
		die("rsync: pulled c4m but cannot parse: %v", err)
	}
	local, _ := openLocalTreeStore()

	remoteHost, _ := splitRemote(remoteSrc)
	remoteStore := ensureTrailingSlash(remoteStorePath(remoteHost))
	layout, err := listRemoteLayout(remoteStore, extra)
	if err != nil {
		die("rsync: %v", err)
	}

	fmt.Fprintf(os.Stderr, "c4sh: rsync: pulling the store objects %s names ...\n", filepath.Base(localC4m))
	fetched, present := 0, 0
	asked := make(map[c4.ID]bool)
	var missing []string
	for {
		var want []object
		for _, o := range referencedObjects(m, local) {
			if asked[o.id] {
				continue
			}
			asked[o.id] = true
			if local.Has(o.id) {
				present++
				continue
			}
			want = append(want, o)
		}
		if len(want) == 0 {
			break
		}
		got, err := fetchObjects(want, layout, remoteStore, local, extra)
		if err != nil {
			die("rsync: store transfer failed: %v", err)
		}
		fetched += len(got)
		for _, o := range want {
			if !got[o.id] && o.required {
				missing = append(missing, o.name+" "+o.id.String())
			}
		}
	}
	fmt.Fprintf(os.Stderr, "c4sh: rsync: fetched %d objects, %d already here\n", fetched, present)
	reportMissing(missing, "not in the remote store")
	fmt.Fprintf(os.Stderr, "c4sh: rsync: done\n")
	if len(missing) > 0 {
		osExit(1)
	}
}

// object is one store object a c4m names: its C4 ID, the path in the c4m
// that names it, and whether the c4m depends on it. File content is
// always needed. A listing or range ID list is needed when the c4m doesn't
// carry it itself (a folder listed without its children, a range whose
// list isn't inline); otherwise it is sent when the store has it, as
// c4 id -s stores it, and skipped when it doesn't.
type object struct {
	id       c4.ID
	name     string
	required bool
}

// referencedObjects returns, once each, the store objects m names, reading
// on through every listing and range ID list src holds: the objects a
// one-level folder line names come from its listing, and a range's frames
// from its ID list, as pool finds them (poolNeedsObject, rangeFrames).
func referencedObjects(m *c4m.Manifest, src c4store.Store) []object {
	var out []object
	seen := make(map[c4.ID]bool)
	add := func(id c4.ID, name string, required bool) {
		if id.IsNil() || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, object{id, name, required})
	}
	var walk func(m *c4m.Manifest, base string, depth int)
	walk = func(m *c4m.Manifest, base string, depth int) {
		for _, e := range m.Entries {
			if e.C4ID.IsNil() {
				continue
			}
			name := path.Join(base, strings.TrimSuffix(m.EntryPath(e), "/"))
			switch {
			case e.IsDir():
				if e.C4ID == emptyID {
					continue
				}
				needed := poolNeedsObject(m, e)
				add(e.C4ID, name+"/", needed)
				if needed && depth < maxListingDepth {
					if sub := readListing(src, e.C4ID); sub != nil {
						walk(sub, name, depth+1)
					}
				}
			case e.IsSequence:
				add(e.C4ID, name, poolNeedsObject(m, e))
				dir := path.Dir(name)
				for _, f := range rangeFrames(m, e, src) {
					add(f.C4ID, path.Join(dir, f.Name), true)
				}
			case e.IsSymlink():
				// A link's C4 ID is its target's; the target is sent
				// under its own name when the c4m holds it.
				add(e.C4ID, name, false)
			default:
				add(e.C4ID, name, true)
			}
		}
	}
	walk(m, "", 0)
	return out
}

// fetchBatch is how many places one rsync run of a pull asks for.
const fetchBatch = 200

// maxListingDepth bounds how deep referencedObjects reads listings out of
// the store, so a listing that names itself can't loop.
const maxListingDepth = 1024

// readListing decodes the listing src holds under id, or returns nil when
// src doesn't have it or it isn't c4m text.
func readListing(src c4store.Store, id c4.ID) *c4m.Manifest {
	rc, err := src.Open(id)
	if err != nil {
		return nil
	}
	defer rc.Close()
	sub, err := c4m.NewDecoder(rc).Decode()
	if err != nil {
		return nil
	}
	return sub
}

// contentPather is a store that can say where an object's file is.
type contentPather interface {
	ContentPath(id c4.ID) (string, bool)
}

// openLocalTreeStore opens the configured store and checks that it is a
// local filesystem store, which rsync can read and write.
func openLocalTreeStore() (c4store.Store, contentPather) {
	store, err := openStore()
	if err != nil || store == nil {
		die("rsync: no local store configured (set C4_STORE)")
	}
	cp, ok := store.(contentPather)
	if !ok || storeRoot(store) == "" {
		die("rsync: requires a local filesystem store (S3 stores cannot be rsynced)")
	}
	return store, cp
}

// remoteLayout is the set of folders in a remote store, by their path
// relative to the store's root ("" is the root itself).
type remoteLayout struct {
	// exists is false when the remote has no store at that path yet.
	exists bool
	dirs   map[string]bool
	// fold maps each folder's lower-case path to the path as the remote
	// spells it. A store made on a case-insensitive filesystem holds IDs
	// differing only in case under one folder, named by whichever came
	// first ("c4/1B" holding "c41b…"); the store's lookup descends into it
	// for either case, and so does place.
	fold map[string]string
}

// place returns where id belongs in the remote store, relative to its
// root: the deepest folder of id's trie path (its 2-character segments,
// "c4", then characters 3-4, and so on) that exists there.
func (l remoteLayout) place(id c4.ID) string {
	s := id.String()
	dir := ""
	for i := 0; i+2 <= len(s); i += 2 {
		next := path.Join(dir, s[i:i+2])
		if l.dirs[next] {
			dir = next
			continue
		}
		if v, ok := l.fold[strings.ToLower(next)]; ok {
			dir = v
			continue
		}
		break
	}
	return path.Join(dir, s)
}

// levels returns every place id may be in the remote store, root first:
// the file at each level of its trie path. A split moves a folder's files
// one level down one at a time, so an object can be a level or more above
// its deepest folder (the store's lookup looks at every level, too).
func (l remoteLayout) levels(id c4.ID) []string {
	s := id.String()
	dir := ""
	out := []string{s}
	for i := 0; i+2 <= len(s); i += 2 {
		next := path.Join(dir, s[i:i+2])
		if !l.dirs[next] {
			v, ok := l.fold[strings.ToLower(next)]
			if !ok {
				break
			}
			next = v
		}
		dir = next
		out = append(out, path.Join(dir, s))
	}
	return out
}

// listRemoteLayout lists the folders of the remote store (folders only:
// no file is listed or read). A remote with no store yet has an empty
// layout, and what is pushed lands at the root of a new, flat store.
func listRemoteLayout(remoteStore string, extra []string) (remoteLayout, error) {
	l := remoteLayout{exists: true, dirs: map[string]bool{"": true}, fold: map[string]string{}}
	args := append([]string{"--list-only", "-r", "--include=*/", "--exclude=*", remoteStore}, extra...)
	cmd := exec.Command("rsync", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "No such file or directory") {
			l.exists = false
			return l, nil
		}
		os.Stderr.Write(stderr.Bytes())
		return l, fmt.Errorf("list remote store %s: %v", remoteStore, err)
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "d") {
			continue
		}
		name := strings.TrimSuffix(fields[len(fields)-1], "/")
		if name == "." || name == "" {
			continue
		}
		l.dirs[name] = true
		if _, ok := l.fold[strings.ToLower(name)]; !ok {
			l.fold[strings.ToLower(name)] = name
		}
	}
	return l, nil
}

// newStoreDest returns where to send a new remote store's objects: the
// store's path as a prefix inside the staging folder, and the remote folder
// that prefix is relative to (the remote home, or / for an absolute path).
func newStoreDest(hostColon, storePath string) (prefix, dest string) {
	p := strings.TrimSuffix(storePath, "/")
	switch {
	case strings.HasPrefix(p, "/"):
		return strings.TrimPrefix(p, "/"), hostColon + "/"
	case strings.HasPrefix(p, "~/"):
		return strings.TrimPrefix(p, "~/"), hostColon
	}
	return p, hostColon
}

// fetchObjects copies the wanted objects from the remote store into a
// staging folder, first from each one's deepest folder and then, for any
// not found there, from every level above it, and puts each into the local
// store. It returns the IDs now in the local store.
//
// Each place is named as a source of its own (host:store/c4/XX/<ID>), a
// batch at a time, and what arrives is recognised by name: rsync and the
// openrsync macOS ships agree on that form, where openrsync drops a
// --files-from list for a remote sender.
func fetchObjects(want []object, layout remoteLayout, remoteStore string, local c4store.Store, extra []string) (map[c4.ID]bool, error) {
	stage, err := os.MkdirTemp("", "c4sh-rsync-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	got := make(map[c4.ID]bool)
	pass := func(places map[string]c4.ID) error {
		var list []string
		for p := range places {
			list = append(list, p)
		}
		sort.Strings(list)
		wanted := make(map[string]c4.ID)
		for _, id := range places {
			wanted[id.String()] = id
		}
		into := filepath.Join(stage, "in")
		if err := os.MkdirAll(into, 0755); err != nil {
			return err
		}
		for len(list) > 0 {
			n := min(len(list), fetchBatch)
			args := []string{}
			for _, p := range list[:n] {
				args = append(args, remoteStore+p)
			}
			list = list[n:]
			args = append(append(args, into+"/"), extra...)
			// Exit 23 is rsync's "some files were not transferred": a place
			// where an object isn't. What arrived is checked below.
			if err := runExternalRsyncQuiet(args); err != nil && exitCode(err) != 23 {
				return err
			}
		}
		entries, err := os.ReadDir(into)
		if err != nil {
			return err
		}
		for _, de := range entries {
			id, ok := wanted[de.Name()]
			if !ok || got[id] {
				continue
			}
			f, err := os.Open(filepath.Join(into, de.Name()))
			if err != nil {
				continue
			}
			put, err := local.Put(f)
			f.Close()
			if err != nil {
				return fmt.Errorf("store %s: %v", id, err)
			}
			if put != id {
				fmt.Fprintf(os.Stderr, "c4sh: rsync: remote object %s holds bytes whose C4 ID is %s; not used\n", id, put)
				continue
			}
			got[id] = true
		}
		return os.RemoveAll(into)
	}

	first := make(map[string]c4.ID)
	for _, o := range want {
		first[layout.place(o.id)] = o.id
	}
	if err := pass(first); err != nil {
		return got, err
	}
	rest := make(map[string]c4.ID)
	for _, o := range want {
		if got[o.id] {
			continue
		}
		for _, p := range layout.levels(o.id) {
			if _, tried := first[p]; !tried {
				rest[p] = o.id
			}
		}
	}
	if len(rest) > 0 {
		if err := pass(rest); err != nil {
			return got, err
		}
	}
	return got, nil
}

// reportMissing names each object the c4m needs that couldn't be sent or
// fetched.
func reportMissing(missing []string, why string) {
	for _, miss := range missing {
		fmt.Fprintf(os.Stderr, "c4sh: rsync: missing %s (%s)\n", miss, why)
	}
}

// runRsyncCounting runs rsync with --out-format=%n and returns how many
// store objects it transferred: the lines naming a file whose name is a
// C4 ID. Every other line is passed through.
func runRsyncCounting(args []string) (int, error) {
	cmd := exec.Command("rsync", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if _, perr := c4.Parse(path.Base(strings.TrimSpace(line))); perr == nil {
			n++
			continue
		}
		if line != "" && !strings.HasSuffix(line, "/") {
			fmt.Fprintln(os.Stdout, line)
		}
	}
	return n, err
}

// runExternalRsyncQuiet runs rsync with its error lines kept back: a pull
// asks for an object at places where it may not be, and those misses
// (exit 23) are expected. Any other failure shows rsync's error lines.
func runExternalRsyncQuiet(args []string) error {
	cmd := exec.Command("rsync", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && exitCode(err) != 23 {
		os.Stderr.Write(stderr.Bytes())
	}
	return err
}

// exitCode returns a command's exit status, or -1 when it didn't run.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
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
