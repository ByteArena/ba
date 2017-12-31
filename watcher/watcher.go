package watcher

import (
	"fmt"
	"io/ioutil"
	"path"

	"github.com/fsnotify/fsnotify"
	bettererrors "github.com/xtuc/better-errors"
)

var (
	WATCH_DIR_RECURSION_DEPTH = uint(100)
	WATCH_IGNORE_DIRS         = map[string]bool{
		".git": true,
	}
)

type Watcher struct {
	fsnotifyWatcher *fsnotify.Watcher
	notify          chan error
}

func MakeWatcher() (Watcher, error) {
	watcher, err := fsnotify.NewWatcher()

	if err != nil {
		return Watcher{}, bettererrors.NewFromErr(err)
	}

	notifychan := make(chan error)

	return Watcher{
		fsnotifyWatcher: watcher,
		notify:          notifychan,
	}, nil
}

func (w Watcher) Add(dir string) {
	go func() {
		for {
			select {
			case event := <-w.fsnotifyWatcher.Events:

				if event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create ||
					event.Op&fsnotify.Remove == fsnotify.Remove {

					select {
					case w.notify <- nil: // ok
					default:
						fmt.Println("Already building ignoring")
					}
				}
			case err := <-w.fsnotifyWatcher.Errors:
				w.notify <- bettererrors.NewFromErr(err)
				return
			}
		}
	}()

	err := w.fsnotifyWatcher.Add(dir)

	if err != nil {
		w.notify <- bettererrors.NewFromErr(err)
		return
	}

	err = addDirWatchers(w.fsnotifyWatcher, dir, 0)

	if err != nil {
		w.notify <- err
	}
}

func (w Watcher) Close() error {
	return w.fsnotifyWatcher.Close()
}

func addDirWatchers(watcher *fsnotify.Watcher, dir string, detph uint) error {
	files, err := ioutil.ReadDir(dir)

	if err != nil {
		return bettererrors.NewFromErr(err)
	}

	for _, file := range files {

		if file.IsDir() {
			if _, isIgnored := WATCH_IGNORE_DIRS[file.Name()]; isIgnored {
				return nil
			}

			absName := path.Join(dir, file.Name())

			err := watcher.Add(absName)

			if err != nil {
				return bettererrors.NewFromErr(err)
			}

			if detph < WATCH_DIR_RECURSION_DEPTH {
				err := addDirWatchers(watcher, absName, detph+1)

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (w Watcher) Wait() chan error {
	return w.notify
}
