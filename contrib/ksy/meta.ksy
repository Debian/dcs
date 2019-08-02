meta:
  id: meta
  file-extension: meta
  endian: le

seq:
  - id: entries
    type: metaentry
    repeat: eos

types:
  metaentry:
    seq:
      - id: trigram_raw
        type: str
        encoding: ASCII
        size: 4
      - id: num_entries
        type: u4
      - id: offset_data
        type: u8
    instances:
      trigram:
        value: trigram_raw.reverse
