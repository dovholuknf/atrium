package daemon

import ()

// raw is every byte a ring holds, oldest first, with none of the line-start
// trimming `from` applies, so it can be compared against a model byte for
// byte.
func (r *ringBuffer) raw() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return append(append([]byte{}, r.data[r.at:]...), r.data[:r.at]...)
	}
	return append([]byte{}, r.data[:r.at]...)
}
