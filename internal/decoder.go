package internal

import (
	"fmt"

	"github.com/hraban/opus"
)

const (
	sampleRate      = 48000
	channels        = 1
	frameSizeMs     = 20
	samplesPerFrame = (sampleRate * frameSizeMs) / 1000
)

var globalDec *opus.Decoder

func init() {
    globalDec, _ = opus.NewDecoder(48000, 1)
}

func Decoder(data []byte) ([]int16, error) {
    pcm := make([]int16, 960) 
    n, err := globalDec.Decode(data, pcm)
    if err != nil {
        return nil, err
    }
    
    if n > 0 {
        fmt.Printf("Декодировано сэмплов: %d, данные: %v\n", n, pcm[:n])
    }
    return pcm[:n], nil
}