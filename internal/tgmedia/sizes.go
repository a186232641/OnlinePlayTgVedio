// Package tgmedia holds the small pieces of Telegram media inspection that
// both the indexer (writing rows during sync) and the video/photo serving paths
// (re-resolving an expired locator) need, so neither has to import the other.
package tgmedia

import "github.com/gotd/td/tg"

// SizePick is one downloadable size of a photo: its type letter (what goes into
// tg.InputPhotoFileLocation.ThumbSize) plus the dimensions/bytes we store.
type SizePick struct {
	Type   string
	W, H   int
	Bytes  int64
	Usable bool
}

// thumbMinWidth is the width we aim for in a grid thumbnail. Telegram's "m"
// size is 320px, which is what every client uses for media grids.
const thumbMinWidth = 200

// sizeOf normalizes one PhotoSizeClass into a SizePick. Only the variants that
// can actually be fetched with upload.getFile are Usable: stripped ("i") and
// path ("j") sizes are inline placeholder blobs, and cached sizes carry their
// bytes inline rather than being addressable by type.
func sizeOf(s tg.PhotoSizeClass) SizePick {
	switch v := s.(type) {
	case *tg.PhotoSize:
		return SizePick{Type: v.Type, W: v.W, H: v.H, Bytes: int64(v.Size), Usable: true}
	case *tg.PhotoSizeProgressive:
		// Progressive JPEG: Sizes holds the byte offsets of each progressive
		// scan; the last one is the full length of the image.
		var b int64
		if n := len(v.Sizes); n > 0 {
			b = int64(v.Sizes[n-1])
		}
		return SizePick{Type: v.Type, W: v.W, H: v.H, Bytes: b, Usable: true}
	case *tg.PhotoCachedSize:
		return SizePick{Type: v.Type, W: v.W, H: v.H, Bytes: int64(len(v.Bytes))}
	case *tg.PhotoStrippedSize:
		return SizePick{Type: v.Type, Bytes: int64(len(v.Bytes))}
	default:
		return SizePick{}
	}
}

// PickPhotoSizes chooses the size we serve as the full image (largest usable)
// and the one we serve as the grid thumbnail (smallest usable that is still at
// least thumbMinWidth wide; the smallest usable one if none reaches it).
// Either may come back with an empty Type when the photo has no usable size.
func PickPhotoSizes(sizes []tg.PhotoSizeClass) (full, thumb SizePick) {
	for _, s := range sizes {
		p := sizeOf(s)
		if !p.Usable {
			continue
		}
		if full.Type == "" || p.W > full.W {
			full = p
		}
		switch {
		case thumb.Type == "":
			thumb = p
		case thumb.W < thumbMinWidth:
			// Current pick is too small — anything wider is an improvement.
			if p.W > thumb.W {
				thumb = p
			}
		case p.W >= thumbMinWidth && p.W < thumb.W:
			thumb = p
		}
	}
	return full, thumb
}

// PickDocThumb returns the type letter of the best thumbnail attached to a
// document (videos carry one or two small JPEGs). Empty when the document has
// no fetchable thumbnail — plenty of forwarded files don't.
func PickDocThumb(thumbs []tg.PhotoSizeClass) string {
	var best SizePick
	for _, s := range thumbs {
		p := sizeOf(s)
		if !p.Usable {
			continue
		}
		if best.Type == "" || p.W > best.W {
			best = p
		}
	}
	return best.Type
}
