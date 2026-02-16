package tool

import (
	"context"
	"fmt"
	"os"
)

const maxReadSize = 1 << 20 // 1 MiB

// ReadTool reads file contents.
type ReadTool struct{}

func (ReadTool) Name() string { return "read" }

func (ReadTool) Execute(ctx context.Context, args map[string]any) Result {
	pathVal, ok := args["path"]
	if !ok {
		return Result{Output: "missing required argument: path", IsError: true}
	}

	path, ok := pathVal.(string)
	if !ok {
		return Result{Output: "argument 'path' must be a string", IsError: true}
	}

	if path == "" {
		return Result{Output: "argument 'path' must not be empty", IsError: true}
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Output: fmt.Sprintf("file not found: %s", path), IsError: true}
		}
		return Result{Output: fmt.Sprintf("error accessing file: %v", err), IsError: true}
	}

	if info.IsDir() {
		return Result{Output: fmt.Sprintf("path is a directory: %s", path), IsError: true}
	}

	if info.Size() > maxReadSize {
		return Result{Output: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxReadSize), IsError: true}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Output: fmt.Sprintf("error reading file: %v", err), IsError: true}
	}

	return Result{Output: string(data), IsError: false}
}
