package emulator

import (
	"image"
	"image/color"
)

// PPU Picture Processing Unit
type PPU struct {
	RAM          [0x4000]byte
	sRAM         [0x100]byte // Sprite RAM
	mirror       bool        // 0: 水平ミラー, 1:垂直ミラー
	ptr          uint16      // PPURAMのポインタ 0x2006に書き込まれたとき更新される
	ppudataBuf   byte        // PPUDATAからreadしたときのbuffer
	scroll       [2]uint8    // (水平スクロールpixel, 垂直スクロールpixel)
	scrollFlag   bool        // trueなら2回目として書き込みする
	raster       uint16
	displayImage *image.RGBA // 256*256のイメージデータ
}

// setBGTile 画面の(x,y)ブロックのRGBAの出力を行う
func (cpu *CPU) setBGTile(x, y, scrollPixelX, scrollPixelY uint, mainScreen byte) {
	var spriteNum uint
	var attr byte

	switch mainScreen {
	case 1:
		scrollPixelX += width
	case 2:
		scrollPixelY += height
	case 3:
		scrollPixelX += width
		scrollPixelY += height
	}

	var blockX, blockY uint
	if (scrollPixelX+x*8 >= width && scrollPixelX+x*8 < width*2) && (scrollPixelY+y*8 >= height && scrollPixelY+y*8 < height*2) {
		blockX, blockY = (x*8-width+scrollPixelX)/8, (y*8-height+scrollPixelY)/8
		spriteNum = uint(cpu.PPU.RAM[0x2c00+blockX+blockY*0x20])
		attr = cpu.PPU.RAM[0x2fc0+blockX/4+blockY/4*0x08]
	} else if (scrollPixelX+x*8 >= width && scrollPixelX+x*8 < width*2) && scrollPixelY+y*8 < height {
		blockX, blockY = (x*8-width+scrollPixelX)/8, y+scrollPixelY/8
		spriteNum = uint(cpu.PPU.RAM[0x2400+blockX+blockY*0x20])
		attr = cpu.PPU.RAM[0x27c0+blockX/4+blockY/4*0x08]
	} else if scrollPixelX+x*8 < width && (scrollPixelY+y*8 >= height && scrollPixelY+y*8 < height*2) {
		blockX, blockY = (x*8+scrollPixelX)/8, (y*8-height+scrollPixelY)/8
		spriteNum = uint(cpu.PPU.RAM[0x2800+blockX+blockY*0x20])
		attr = cpu.PPU.RAM[0x2bc0+blockX/4+blockY/4*0x08]
	} else {
		blockX, blockY = ((x*8+scrollPixelX)/8)%(width/8*2), ((y*8+scrollPixelY)/8)%(height/8*2)
		spriteNum = uint(cpu.PPU.RAM[0x2000+blockX+blockY*0x20])
		attr = cpu.PPU.RAM[0x23c0+blockX/4+blockY/4*0x08]
	}

	var pallete byte
	if (blockX%4 < 2) && (blockY%4 < 2) {
		pallete = attr & 0x03
	} else if (blockX%4 >= 2) && (blockY%4 < 2) {
		pallete = (attr & 0x0c) >> 2
	} else if (blockX%4 < 2) && (blockY%4 >= 2) {
		pallete = (attr & 0x30) >> 4
	} else {
		pallete = (attr & 0xc0) >> 6
	}

	// この時点でspriteNum, pallete, 画面位置の情報が手に入っている
	var spriteBytes [16]byte
	baseAddr := cpu.getBaseAddr("BG")
	for i := 0; i < 16; i++ {
		spriteBytes[i] = cpu.PPU.RAM[baseAddr+uint(spriteNum)*16+uint(i)]
	}

	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			color0 := (spriteBytes[row] & (0x01 << (7 - column))) >> (7 - column)
			color1 := ((spriteBytes[row+8] & (0x01 << (7 - column))) >> (7 - column)) << 1

			p := uint(pallete*4) + uint(color0+color1) // パレット番号 + パレット内番号
			if p%4 == 0 {
				p = 0x10
			}

			R, G, B := colors[cpu.PPU.RAM[0x3f00+p]][0], colors[cpu.PPU.RAM[0x3f00+p]][1], colors[cpu.PPU.RAM[0x3f00+p]][2]
			pixelX := int(x*8 + uint(column) - (scrollPixelX % 8))
			pixelY := int(y*8 + uint(row) - (scrollPixelY % 8))
			cpu.PPU.displayImage.Set(pixelX, pixelY, color.RGBA{R, G, B, 0xff})
		}
	}
}

// setSPRTile
func (cpu *CPU) setSPRTile(pixelX, pixelY, spriteNum, attr byte) {
	pallete := attr & 0x03 // パレット番号
	lrTurn := attr & 0x40  // 左右反転
	udTurn := attr & 0x80  // 上下反転

	baseAddr := cpu.getBaseAddr("SPR")
	var spriteBytes [16]byte
	for i := 0; i < 16; i++ {
		spriteBytes[i] = cpu.PPU.RAM[baseAddr+uint(spriteNum)*16+uint(i)]
	}

	if lrTurn != 0 && udTurn != 0 {
		// 上下左右反転
		for h := 0; h < 8; h++ {
			for w := 0; w < 8; w++ {
				color0 := (spriteBytes[7-h] & (0x01 << w)) >> w
				color1 := (spriteBytes[15-h] & (0x01 << w) >> w) << 1

				p := int(pallete*4) + int(color0+color1) // パレット番号 + パレット内番号

				// パレットミラーリング
				isTransparent := false
				if p == 0 || p == 4 || p == 8 || p == 12 {
					isTransparent = true
				}

				if !isTransparent {
					R, G, B := colors[cpu.PPU.RAM[0x3f10+p]][0], colors[cpu.PPU.RAM[0x3f10+p]][1], colors[cpu.PPU.RAM[0x3f10+p]][2]
					x := int(pixelX) + w
					y := int(pixelY) + h
					c := color.RGBA{R, G, B, 0xff}
					cpu.PPU.displayImage.Set(x, y, c)
				}
			}
		}
	} else if lrTurn == 0 && udTurn != 0 {
		// 上下反転
		for h := 0; h < 8; h++ {
			for w := 0; w < 8; w++ {
				color0 := (spriteBytes[7-h] & (0x01 << (7 - w))) >> (7 - w)
				color1 := ((spriteBytes[15-h] & (0x01 << (7 - w))) >> (7 - w)) << 1

				p := int(pallete*4) + int(color0+color1) // パレット番号 + パレット内番号

				// パレットミラーリング
				isTransparent := false
				if p == 0 || p == 4 || p == 8 || p == 12 {
					isTransparent = true
				}

				if !isTransparent {
					R, G, B := colors[cpu.PPU.RAM[0x3f10+p]][0], colors[cpu.PPU.RAM[0x3f10+p]][1], colors[cpu.PPU.RAM[0x3f10+p]][2]
					x := int(pixelX) + w
					y := int(pixelY) + h
					c := color.RGBA{R, G, B, 0xff}
					cpu.PPU.displayImage.Set(x, y, c)
				}
			}
		}
	} else if lrTurn != 0 && udTurn == 0 {
		// 左右反転
		for h := 0; h < 8; h++ {
			for w := 0; w < 8; w++ {
				color0 := (spriteBytes[h] & (0x01 << w)) >> w
				color1 := (spriteBytes[h+8] & (0x01 << w) >> w) << 1

				p := int(pallete*4) + int(color0+color1) // パレット番号 + パレット内番号

				// パレットミラーリング
				isTransparent := false
				if p == 0 || p == 4 || p == 8 || p == 12 {
					isTransparent = true
				}

				if !isTransparent {
					R, G, B := colors[cpu.PPU.RAM[0x3f10+p]][0], colors[cpu.PPU.RAM[0x3f10+p]][1], colors[cpu.PPU.RAM[0x3f10+p]][2]
					x := int(pixelX) + w
					y := int(pixelY) + h
					c := color.RGBA{R, G, B, 0xff}
					cpu.PPU.displayImage.Set(x, y, c)
				}
			}
		}
	} else {
		// 反転無し
		for h := 0; h < 8; h++ {
			for w := 0; w < 8; w++ {
				color0 := (spriteBytes[h] & (0x01 << (7 - w))) >> (7 - w)
				color1 := ((spriteBytes[h+8] & (0x01 << (7 - w))) >> (7 - w)) << 1

				p := int(pallete*4) + int(color0+color1) // パレット番号 + パレット内番号

				// パレットミラーリング
				isTransparent := false
				if p == 0 || p == 4 || p == 8 || p == 12 {
					isTransparent = true
				}

				if !isTransparent {
					R, G, B := colors[cpu.PPU.RAM[0x3f10+p]][0], colors[cpu.PPU.RAM[0x3f10+p]][1], colors[cpu.PPU.RAM[0x3f10+p]][2]
					x := int(pixelX) + w
					y := int(pixelY) + h
					c := color.RGBA{R, G, B, 0xff}
					cpu.PPU.displayImage.Set(x, y, c)
				}
			}
		}
	}
}

func (cpu *CPU) getBaseAddr(name string) uint {
	if name == "BG" {
		if cpu.RAM[0x2000]&0x10 > 0 {
			return 0x1000
		}
	} else if name == "SPR" {
		if cpu.RAM[0x2000]&0x08 > 0 {
			return 0x1000
		}
	}
	return 0x0000
}
