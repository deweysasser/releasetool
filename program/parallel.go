package program

import (
	"github.com/hashicorp/go-multierror"
	"sync"
)

func parallel[T any](list []T, f func(t T) error) error {
	errors := make(chan error)

	go func() {
		wg := sync.WaitGroup{}
		defer close(errors)
		defer wg.Wait()
		wg.Add(len(list))

		for _, r := range list {
			go func(r T) {
				defer wg.Done()
				if err := f(r); err != nil {
					errors <- err
				}
			}(r)
		}
	}()

	var err error

	for e := range errors {
		err = multierror.Append(err, e)
	}

	return err
}
