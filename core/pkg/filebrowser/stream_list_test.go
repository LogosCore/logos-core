package filebrowser

import (
	"testing"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/models"
)

// A stream counts as downloaded only when core kept its copy whole.
func TestStreamList(t *testing.T) {
	id := uuid.New()
	for _, c := range []struct {
		kept *models.MinionFile
		want string
	}{
		{nil, models.FsStreamed},
		{&models.MinionFile{FileID: id, State: models.MinionFileReceiving}, models.FsStreamed},
		{&models.MinionFile{FileID: id, State: models.MinionFileCancelled}, models.FsStreamed},
		{&models.MinionFile{FileID: id, State: models.MinionFileFailed}, models.FsStreamed},
		{&models.MinionFile{FileID: id, State: models.MinionFileJoining}, models.FsDownloaded},
		{&models.MinionFile{FileID: id, State: models.MinionFileComplete}, models.FsDownloaded},
	} {
		list, file := streamList(c.kept)
		if list != c.want || (list == models.FsDownloaded) != (file != nil && *file == id) {
			t.Errorf("%+v: %s %v, want %s", c.kept, list, file, c.want)
		}
	}
}
