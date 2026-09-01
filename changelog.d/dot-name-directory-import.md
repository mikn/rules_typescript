### Fixed

- **A directory import whose last segment is a dot-name (`./tools/.internal`)
  no longer loses its dep.** `path.Ext` takes everything from the final dot, and
  a dot-name has no other dot, so the whole segment read as an unclassified file
  extension and the guard for those dropped the label before any package check
  ran -- the checked-in-`BUILD`-file check included. A dot-name directory with
  no BUILD file is still dropped, now by that check rather than this one.
