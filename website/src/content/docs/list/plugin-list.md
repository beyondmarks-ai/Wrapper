---
title: Plugin List
description: Complete list of available wrapper plugins
head:
  - tag: title
    content: Plugin List | wrapper
---

Wrapper supports various plugins to extend its functionality. Below is a complete list of available plugins and their requirements.

### Metadata

- **Description:** Show more detailed metadata for files and directories

- **Requirements:** [`exiftool`](https://exiftool.org)

- **Config name:** `metadata`

### MD5 Checksum

- **Description:** Show MD5 checksums for regular files in the metadata panel

- **Requirements:** None

- **Config name:** `enable_md5_checksum`

- **Note:** Calculating checksums reads the selected file, so it may be slow for large files.

### Zoxide

- **Description:** Smart directory jumping integration with zoxide. Navigate to frequently used directories quickly with a searchable modal interface.

- **Requirements:** [`zoxide`](https://github.com/ajeetdsouza/zoxide)

- **Config name:** `zoxide_support`

- **Usage:** Press `z` to open the zoxide navigation modal. Start typing to search directories, use arrow keys to navigate results, and press Enter to jump to a directory.

### Everything Global Search (Windows)

- **Description:** Searches the existing Everything filename index from a keyboard-driven modal and reveals the selected result in the current file panel.

- **Requirements:** [Everything](https://www.voidtools.com/) must be running. Place the matching architecture DLL from the [Everything SDK](https://www.voidtools.com/support/everything/sdk/) beside `wrap.exe`.

- **Usage:** Press `Ctrl+G`, type a file or folder name, navigate with the arrow keys, and press Enter.
