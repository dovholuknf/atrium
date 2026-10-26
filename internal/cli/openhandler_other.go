//go:build !windows

package cli

import "errors"

var errOpenHandlerWindows = errors.New("open-handler registers a link with Windows. other systems open through the room")

func registerOpenHandler(string) error { return errOpenHandlerWindows }

func unregisterOpenHandler() error { return errOpenHandlerWindows }
