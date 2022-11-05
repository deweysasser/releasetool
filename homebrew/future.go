package homebrew

import "sync"

func future[T any](f func() (T, error)) func() (T, error) {
	var val T
	var e error
	o := sync.Once{}

	return func() (T, error) {

		o.Do(func() {
			val, e = f()
		})

		return val, e
	}
}
