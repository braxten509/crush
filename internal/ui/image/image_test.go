package image

import (
	"image"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResetCache(t *testing.T) {
	t.Parallel()

	cachedMutex.Lock()
	cachedImages[imageKey{id: "a", cols: 10, rows: 10}] = cachedImage{
		img:  image.NewRGBA(image.Rect(0, 0, 1, 1)),
		cols: 10,
		rows: 10,
	}
	cachedImages[imageKey{id: "b", cols: 20, rows: 20}] = cachedImage{
		img:  image.NewRGBA(image.Rect(0, 0, 1, 1)),
		cols: 20,
		rows: 20,
	}
	cachedMutex.Unlock()

	ResetCache()

	cachedMutex.RLock()
	length := len(cachedImages)
	cachedMutex.RUnlock()

	require.Equal(t, 0, length)
}

func TestResetIdempotent(t *testing.T) {
	t.Parallel()

	// Calling Reset on an empty cache should not panic.
	ResetCache()

	cachedMutex.RLock()
	length := len(cachedImages)
	cachedMutex.RUnlock()

	require.Equal(t, 0, length)
}

// Not parallel: the other tests clear the shared cache.
func TestBlocksArePaintedOnce(t *testing.T) {
	key := imageKey{id: "painted-once", cols: 4, rows: 2}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	cachedMutex.Lock()
	cachedImages[key] = cachedImage{img: img, cols: 4, rows: 2}
	cachedMutex.Unlock()
	t.Cleanup(ResetCache)

	first := EncodingBlocks.Render(key.id, key.cols, key.rows)
	require.NotEmpty(t, first)
	cachedMutex.RLock()
	kept := cachedImages[key].blocks
	cachedMutex.RUnlock()
	require.Equal(t, first, kept, "the painted picture is kept")
	require.Equal(t, first, EncodingBlocks.Render(key.id, key.cols, key.rows))
}
