package json

import (
	"fmt"
	"io"
	"math"
	"sync"
)

const (
	// The size of []byte slice that we ingest with the ReaderAt interface
	readerBlockSize = 1024 * 64
	// Buffer in the channels. This cuts down context switch overhead
	cursorbuffer = 1024 * 3
	// warn for quoted regions longer than this (might indicate bad quote
	// processing in lexChan).
	longStringSize = 1024

	// how long the output block should be compared to lenth of bytesBuffer.
	// This ratio trades off between overallocating and repeated growth.
	charToBytePercentage = 16
)

type char byte

var (
	// overlaps with info for speed: this is just what to store.
	xxkeep = [256]bool{
		'{':  true,
		'}':  true,
		'\\': true,
		'[':  true,
		']':  true,
		'"':  true,
		//		':':  true,
		',': true,
	}
	xxskipOk = [256]bool{
		'{': true,
		'}': true,
		'[': true,
		']': true,
		',': true,
	}

	xxinfos = [256]struct {
		closer    char
		escape    bool
		flat      bool // rename -> literal
		separator bool
	}{
		'{':  {'}', false, false, false},
		'[':  {']', false, false, false},
		'"':  {'"', false, true, false},
		'\\': {0, true, false, false},
		//		':':  {0, false, false, true},
		',': {0, false, false, true},
	} // todo: instead of table, maybe store some function?
)

const (
	xxarrKind      = '['
	xxobjKind      = '{'
	xxtxtKind      = ' '
	xxstxKind char = 2 // ascii start-of-text
)

type charat struct {
	c   char
	idx int64
	err error
}

func (cat charat) String() string {
	if cat.err != nil {
		return fmt.Sprintf("<!%v!>", cat.err)
	}
	return fmt.Sprintf("%d%c", cat.idx, cat.c)
}

// 1. ------

// Byte blocks are raw sections of files. The interface offered by
// Reader and ReaderAt (with us allocating and passing in the []byte slice
// to return data into strongly implies that we are copying data from the
// file into these byte slices.  Which is a shame. Possbily there is
// a zero copy API available, esp for memory mapped access.
// For example, see https://developpaper.com/zero-copy-read-file-into-go-object/
//
// such an api would rather render this stage useless.
//
// but for now, we read from the file into a channel of byte slices.

type byteBlock struct {
	bs     []byte
	offset int64
	err    error
}

// FIXME: see mmapReader in compare_test

// Place in readerCurson chain: 1
func readBlocks(r readerAndReaderAt, blkSz int64) chan byteBlock {
	// TODO: avoid mulitple of cache line size by doing somethign like
	// size = 2^n - 2^k, k << n
	blCh := make(chan byteBlock, cursorbuffer)
	rsize := r.Size()
	var (
		wg     sync.WaitGroup
		cnt    int
		offset int64
	)

	for offset < rsize {
		wg.Add(1)
		go func(count int, off int64) {
			bs := make([]byte, blkSz)
			n, err := r.ReadAt(bs, off)
			if err == nil || err == io.EOF {
				blCh <- byteBlock{bs[:n], off, nil}
			}
			if err != nil && err != io.EOF {
				blCh <- byteBlock{nil, 0, err}
				out("read error: %v", err)
			}
			wg.Done()
		}(cnt, offset)
		cnt += 1
		offset += blkSz
	}
	go func() {
		wg.Wait()
		close(blCh)
	}()

	return blCh
}

// 2. ---------------

// We care only about some runes in the file. Luckily for us, these are
// all standard 7bit chars, so we don't need to think about UTF.
// So we scan the byteBlock and look for these interesting ones. We collect
// these into []charat (channel locking is expensive for small objects).

// Speedup jun 12
// TODO There is 1-1 mapping between byteBlocks and charatBlocks, so passing the
// via channel seems possibly wasteful. Just start a goroutine to calculate the
// charatblock, OR return a function to calculate it on demand.

type charatBlock struct {
	cats       []*charat
	start, end int64
}

func (block byteBlock) scanBlock() charatBlock {
	cats := make([]*charat, 0, (len(block.bs)*charToBytePercentage)/100)
	offset := block.offset
	for i, c := range block.bs {
		if keep[c] {
			cats = append(cats, &charat{char(c), offset + int64(i), nil})
		}
	}
	return charatBlock{cats: cats, start: offset, end: offset + int64(len(block.bs))}
}

// Place in readerCurson chain: 2
func scanBlocks(blockCh chan byteBlock) chan charatBlock {
	outCh := make(chan charatBlock, cursorbuffer)

	var wg sync.WaitGroup

	for byteBl := range blockCh {
		wg.Add(1)
		go func(bbl byteBlock) {
			outCh <- bbl.scanBlock()
			wg.Done()
		}(byteBl)
	}
	go func() {
		wg.Wait()
		close(outCh)
	}()
	return outCh
}

// Place in readerCurson chain: 3
func seqBlocks(catBlocks chan charatBlock) chan charatBlock {
	outCh := make(chan charatBlock, cursorbuffer)
	cats := make(map[int64]charatBlock)
	var next int64
	stream := func() {
		// This buffers blocks in a map by their starting offset,
		// and we track the next starting offset to output.
		// For each incoming block, we see whether it can be output now,
		// and ifso can we cascade to the next...
		for len(cats) > 0 {
			catBl, ok := cats[next]
			if !ok {
				return
			}
			delete(cats, next)
			outCh <- catBl
			next = catBl.end
		}
	}
	go func() {
		for catBl := range catBlocks {
			cats[catBl.start] = catBl
			stream()
		}
		close(outCh)
		if len(cats) > 0 {
			out("next : %d", next)
			for k := range cats {
				out("cat %d", k)
			}
			panic("input consumed, but unreachable charats remain")
		}
	}()

	return outCh
}
