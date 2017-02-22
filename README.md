This is a small program that partially implements a GIF decoder. I wrote it to
help me debug GIF files that other decoders had problems with.

It is mainly useful for seeing the blocks and their sizes.

It does not fully implement the GIF89a specification, and for the most part
does not parse out all of the blocks it does recognize. It also does not have
any LZW decompression capability.
