# Changelog

## 1.0.27 (unreleased)

- ⚠ `c4sh rsync` sent the whole local store, including content the c4m
  does not name, and a pull fetched the whole remote store. A push now
  sends only the objects the c4m names (its files' content, and the
  listings and range ID lists it references), each placed where the
  remote's store looks for it, flat or split. A pull fetches only what the
  pulled c4m names. The counts it prints are what it sent or fetched, and a
  file whose content is missing is named and makes it exit 1.
