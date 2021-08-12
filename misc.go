package json

// misc.go holds ... utility functions that one day may deserve their own package

// For now, mainly dead-simple logging and debugging.

const loglevel = 3 // 0 = error only, 1 = warn, 2 = info, 3 = debug

func dolog(fixe string) {
}

// not a thing already? This is what io.SectionReader implements
type readerAndReaderAt interface {
	io.Reader
	io.ReaderAt
	Size() int64
}
