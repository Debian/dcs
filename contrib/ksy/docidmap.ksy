meta:
  id: docidmap
  file-extension: map
  endian: le

instances:
  index_offset_offset:
    value: _io.size-4
  index_offset:
    pos: index_offset_offset
    type: u4
  index:
    type: entry
    pos: index_offset
    repeat: expr
    repeat-expr: (index_offset_offset-index_offset)/4

types:
  entry:
    seq:
      - id: filename_offset
        type: u4
    instances:
      fn:
        pos: filename_offset
        io: _root._io # treat offset as absolute
        type: strz
        encoding: UTF8
        terminator: 0xa # '\n'
