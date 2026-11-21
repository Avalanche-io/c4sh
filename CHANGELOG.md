# Changelog

## 1.0.27 (unreleased)

- ⚠ `c4sh rsync` sent the whole local store, including content the c4m
  does not name, and a pull fetched the whole remote store. A push now
  sends only the objects the c4m names (its files' content, and the
  listings and range ID lists it references), each placed where the
  remote's store looks for it, flat or split. A pull fetches only what the
  pulled c4m names. The counts it prints are what it sent or fetched, and a
  file whose content is missing is named and makes it exit 1.
- ⚠ `c4sh cp x.c4m: dir/` didn't write a folded range: it stopped with
  "missing content" for the range's own C4 ID. It now writes each frame,
  named from the range and with its own content, so a re-scan with
  `c4 id -S` gives back the c4m's description ID (core's range fix).
- ⚠ `c4sh cp x.c4m: dir/` with a read-only folder in the c4m wrote it
  empty, and into a new target folder wrote nothing: the folder's mode was
  set before its contents. It now writes the contents first and the modes
  last (core's read-only folder fix).
- Built on core release/1.0.27 7d036c5, as a pseudo-version; the suite
  release moves it to v1.0.27.
