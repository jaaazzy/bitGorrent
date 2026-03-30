package main

import (
	"bitGorrent/internal/p2p"
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

/* Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
*/

func (a *App) FileUpload(inPath, outPath string) (string, error) {

	tf, err := p2p.Open(inPath)

	if err != nil {
		return "", err
	}

	err = tf.DownloadToFile(outPath)

	if err != nil {
		return "", err
	}

	return "Download Complete", nil

}

func (a *App) SelectTorrentFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a torrent file",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Torrent Files (*.torrent)",
				Pattern:     "*.torrent",
			},
		},
	})
}

func (a *App) SelectFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select output folder",
	})
}

/*
func (a *App) MagnetLink() (string, error){

}
*/
