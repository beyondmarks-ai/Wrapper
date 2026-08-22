package internal

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	extract "github.com/hashicorp/go-extract"

	"github.com/beyondmarks-ai/Wrapper/src/internal/ui/processbar"
	"github.com/beyondmarks-ai/Wrapper/src/pkg/utils"
)

const (
	maxLocalArchiveInput      = int64(20 << 30)
	maxLocalArchiveExtraction = int64(100 << 30)
	maxLocalArchiveFiles      = int64(100_000)
)

func extractCompressFile(src, dest string, processBar *processbar.Model) error {
	p, err := processBar.SendAddProcessMsg(filepath.Base(src), processbar.OpExtract, 1, true)
	if err != nil {
		return fmt.Errorf("cannot spawn process: %w", err)
	}

	archive, err := os.Open(src)
	if err == nil {
		defer archive.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err = extract.Unpack(ctx, dest, archive, extract.NewConfig(
			extract.WithCreateDestination(true),
			extract.WithCustomCreateDirMode(utils.ExtractedDirMode),
			extract.WithCustomDecompressFileMode(utils.ExtractedFileMode),
			extract.WithDenySymlinkExtraction(true),
			extract.WithMaxFiles(maxLocalArchiveFiles),
			extract.WithMaxInputSize(maxLocalArchiveInput),
			extract.WithMaxExtractionSize(maxLocalArchiveExtraction),
			extract.WithOverwrite(false),
		))
	}
	if err != nil {
		p.State = processbar.Failed
		slog.Error("Error extracting", "path", src, "error", err)
	} else {
		p.State = processbar.Successful
		p.Done = 1
	}

	p.DoneTime = time.Now()
	if updateErr := processBar.SendUpdateProcessMsg(p, true); updateErr != nil {
		slog.Error("Error sending process update", "error", updateErr)
	}
	return err
}
