package machine

import (
	"bytes"
	"context"
	"crypto/des"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"
)

// --- VNC screenshot helpers ---

// RFB protocol constants
const (
	rfbMsgSetPixelFormat           = 0
	rfbMsgFramebufferUpdateRequest = 3
	rfbMsgSetEncodings             = 2
	rfbMsgFramebufferUpdate        = 0
	rfbSecurityNone                = 1
	rfbSecurityVNCAuth             = 2
	rfbEncodingRaw                 = 0
	maxRFBNameLength               = 1 << 20
	maxRFBPixels                   = 64 * 1024 * 1024
	maxRFBRectangles               = 4096
)

// wsChanReader adapts a WebSocket recv channel into an io.Reader.
type wsChanReader struct {
	recv    <-chan []byte
	errs    <-chan error
	buf     []byte
	timeout time.Duration
}

func (r *wsChanReader) Read(p []byte) (int, error) {
	// Drain buffered data first
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	// Wait for next message from WebSocket
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	select {
	case msg, ok := <-r.recv:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, msg)
		if n < len(msg) {
			r.buf = msg[n:]
		}
		return n, nil
	case err, ok := <-r.errs:
		if !ok {
			return 0, io.EOF
		}
		return 0, err
	case <-timer.C:
		return 0, fmt.Errorf("VNC read timeout (%v)", r.timeout)
	}
}

// rfbGrabFrame performs a minimal RFB handshake and captures a single frame.
// vncPassword is the VNC ticket used for VNCAuth (security type 2).
func rfbGrabFrame(ctx context.Context, r io.Reader, send chan<- []byte, vncPassword string) (width, height uint16, pixels []byte, err error) {
	// 1. Version handshake
	verBuf := make([]byte, 12)
	if _, err = io.ReadFull(r, verBuf); err != nil {
		return 0, 0, nil, fmt.Errorf("reading server version: %w", err)
	}
	log.Debugf("RFB server version: %s", strings.TrimSpace(string(verBuf)))
	if err = sendRFB(ctx, send, []byte("RFB 003.008\n")); err != nil {
		return 0, 0, nil, err
	}

	// 2. Security handshake
	var numSecTypes uint8
	if err = binary.Read(r, binary.BigEndian, &numSecTypes); err != nil {
		return 0, 0, nil, fmt.Errorf("reading security type count: %w", err)
	}
	if numSecTypes == 0 {
		// Server sent a reason string for failure
		var reasonLen uint32
		if err = binary.Read(r, binary.BigEndian, &reasonLen); err != nil {
			return 0, 0, nil, fmt.Errorf("reading VNC rejection reason length: %w", err)
		}
		if reasonLen > maxRFBNameLength {
			return 0, 0, nil, fmt.Errorf("VNC rejection reason is too large: %d bytes", reasonLen)
		}
		reason := make([]byte, reasonLen)
		if _, err = io.ReadFull(r, reason); err != nil {
			return 0, 0, nil, fmt.Errorf("reading VNC rejection reason: %w", err)
		}
		return 0, 0, nil, fmt.Errorf("VNC server rejected connection: %s", string(reason))
	}

	secTypes := make([]byte, numSecTypes)
	if _, err = io.ReadFull(r, secTypes); err != nil {
		return 0, 0, nil, fmt.Errorf("reading security types: %w", err)
	}
	log.Debugf("RFB security types offered: %v", secTypes)

	// Prefer None (1), fall back to VNCAuth (2)
	selectedSec := byte(0)
	for _, st := range secTypes {
		if st == rfbSecurityNone {
			selectedSec = rfbSecurityNone
			break
		}
		if st == rfbSecurityVNCAuth {
			selectedSec = rfbSecurityVNCAuth
		}
	}
	if selectedSec == 0 {
		return 0, 0, nil, fmt.Errorf("VNC server offers no supported security types (got: %v)", secTypes)
	}
	if err = sendRFB(ctx, send, []byte{selectedSec}); err != nil {
		return 0, 0, nil, err
	}

	if selectedSec == rfbSecurityVNCAuth {
		// VNC Authentication: server sends 16-byte challenge, client responds with DES-encrypted challenge
		challenge := make([]byte, 16)
		if _, err = io.ReadFull(r, challenge); err != nil {
			return 0, 0, nil, fmt.Errorf("reading VNC auth challenge: %w", err)
		}
		response, encryptErr := vncAuthEncrypt(challenge, vncPassword)
		if encryptErr != nil {
			return 0, 0, nil, encryptErr
		}
		if err = sendRFB(ctx, send, response); err != nil {
			return 0, 0, nil, err
		}
	}

	// Read SecurityResult
	var secResult uint32
	if err = binary.Read(r, binary.BigEndian, &secResult); err != nil {
		return 0, 0, nil, fmt.Errorf("reading security result: %w", err)
	}
	if secResult != 0 {
		return 0, 0, nil, fmt.Errorf("VNC security handshake failed (result=%d)", secResult)
	}

	// 3. ClientInit (shared=true)
	if err = sendRFB(ctx, send, []byte{1}); err != nil {
		return 0, 0, nil, err
	}

	// 4. ServerInit — read framebuffer dimensions and pixel format
	var serverInit struct {
		Width       uint16
		Height      uint16
		PixelFormat [16]byte
		NameLen     uint32
	}
	if err = binary.Read(r, binary.BigEndian, &serverInit); err != nil {
		return 0, 0, nil, fmt.Errorf("reading ServerInit: %w", err)
	}
	width = serverInit.Width
	height = serverInit.Height
	if width == 0 || height == 0 || uint64(width)*uint64(height) > maxRFBPixels {
		return 0, 0, nil, fmt.Errorf("invalid framebuffer dimensions %dx%d", width, height)
	}
	if serverInit.NameLen > maxRFBNameLength {
		return 0, 0, nil, fmt.Errorf("RFB desktop name is too large: %d bytes", serverInit.NameLen)
	}

	// Read and discard the desktop name
	name := make([]byte, serverInit.NameLen)
	if _, err = io.ReadFull(r, name); err != nil {
		return 0, 0, nil, fmt.Errorf("reading desktop name: %w", err)
	}
	log.Debugf("RFB desktop: %s (%dx%d)", string(name), width, height)

	// 5. SetPixelFormat — request 32bpp with R at byte 0, G at byte 1, B at byte 2
	pixFmt := &bytes.Buffer{}
	pixFmt.WriteByte(rfbMsgSetPixelFormat)              // message type
	pixFmt.Write([]byte{0, 0, 0})                       // padding
	pixFmt.WriteByte(32)                                // bits-per-pixel
	pixFmt.WriteByte(24)                                // depth
	pixFmt.WriteByte(0)                                 // big-endian (0=little)
	pixFmt.WriteByte(1)                                 // true-color
	binary.Write(pixFmt, binary.BigEndian, uint16(255)) // red-max
	binary.Write(pixFmt, binary.BigEndian, uint16(255)) // green-max
	binary.Write(pixFmt, binary.BigEndian, uint16(255)) // blue-max
	pixFmt.WriteByte(0)                                 // red-shift
	pixFmt.WriteByte(8)                                 // green-shift
	pixFmt.WriteByte(16)                                // blue-shift
	pixFmt.Write([]byte{0, 0, 0})                       // padding
	if err = sendRFB(ctx, send, pixFmt.Bytes()); err != nil {
		return 0, 0, nil, err
	}

	// 6. SetEncodings — request Raw encoding only
	encMsg := &bytes.Buffer{}
	encMsg.WriteByte(rfbMsgSetEncodings)                          // message type
	encMsg.WriteByte(0)                                           // padding
	binary.Write(encMsg, binary.BigEndian, uint16(1))             // number of encodings
	binary.Write(encMsg, binary.BigEndian, int32(rfbEncodingRaw)) // Raw
	if err = sendRFB(ctx, send, encMsg.Bytes()); err != nil {
		return 0, 0, nil, err
	}

	// 7. FramebufferUpdateRequest — full screen, non-incremental
	fbReq := &bytes.Buffer{}
	fbReq.WriteByte(rfbMsgFramebufferUpdateRequest)
	fbReq.WriteByte(0)                               // incremental = false
	binary.Write(fbReq, binary.BigEndian, uint16(0)) // x
	binary.Write(fbReq, binary.BigEndian, uint16(0)) // y
	binary.Write(fbReq, binary.BigEndian, width)     // width
	binary.Write(fbReq, binary.BigEndian, height)    // height
	if err = sendRFB(ctx, send, fbReq.Bytes()); err != nil {
		return 0, 0, nil, err
	}

	// 8. Read FramebufferUpdate response
	// The server may send other message types first; skip until we get type 0
	for {
		var msgType uint8
		if err = binary.Read(r, binary.BigEndian, &msgType); err != nil {
			return 0, 0, nil, fmt.Errorf("reading message type: %w", err)
		}
		if msgType == rfbMsgFramebufferUpdate {
			break
		}
		// Skip unknown messages — read and discard based on type
		if err = rfbSkipMessage(r, msgType); err != nil {
			return 0, 0, nil, fmt.Errorf("skipping message type %d: %w", msgType, err)
		}
	}

	// Parse FramebufferUpdate header
	var pad uint8
	var numRects uint16
	if err = binary.Read(r, binary.BigEndian, &pad); err != nil {
		return 0, 0, nil, fmt.Errorf("reading framebuffer update padding: %w", err)
	}
	if err = binary.Read(r, binary.BigEndian, &numRects); err != nil {
		return 0, 0, nil, fmt.Errorf("reading rect count: %w", err)
	}
	if numRects > maxRFBRectangles {
		return 0, 0, nil, fmt.Errorf("framebuffer update has too many rectangles: %d", numRects)
	}

	// Read all rectangles — accumulate pixel data
	totalPixels := make([]byte, int(width)*int(height)*4)

	for i := uint16(0); i < numRects; i++ {
		var rect struct {
			X, Y, W, H   uint16
			EncodingType int32
		}
		if err = binary.Read(r, binary.BigEndian, &rect); err != nil {
			return 0, 0, nil, fmt.Errorf("reading rect %d header: %w", i, err)
		}

		if rect.EncodingType != rfbEncodingRaw {
			return 0, 0, nil, fmt.Errorf("unsupported encoding type %d in rect %d", rect.EncodingType, i)
		}
		if uint32(rect.X)+uint32(rect.W) > uint32(width) || uint32(rect.Y)+uint32(rect.H) > uint32(height) {
			return 0, 0, nil, fmt.Errorf("rectangle %d is outside framebuffer bounds", i)
		}

		rectSize := int(rect.W) * int(rect.H) * 4
		rectData := make([]byte, rectSize)
		if _, err = io.ReadFull(r, rectData); err != nil {
			return 0, 0, nil, fmt.Errorf("reading rect %d pixels: %w", i, err)
		}

		// Place rectangle pixels into the full framebuffer at the correct position
		for row := 0; row < int(rect.H); row++ {
			srcOff := row * int(rect.W) * 4
			dstOff := ((int(rect.Y) + row) * int(width) * 4) + (int(rect.X) * 4)
			copy(totalPixels[dstOff:dstOff+int(rect.W)*4], rectData[srcOff:srcOff+int(rect.W)*4])
		}
	}

	return width, height, totalPixels, nil
}

func sendRFB(ctx context.Context, send chan<- []byte, message []byte) error {
	select {
	case send <- message:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("sending RFB message: %w", ctx.Err())
	}
}

// rfbSkipMessage skips over a server-to-client RFB message that isn't a FramebufferUpdate.
func rfbSkipMessage(r io.Reader, msgType uint8) error {
	switch msgType {
	case 1: // SetColourMapEntries
		var header struct {
			Pad        uint8
			FirstColor uint16
			NumColors  uint16
		}
		if err := binary.Read(r, binary.BigEndian, &header); err != nil {
			return err
		}
		skip := make([]byte, int(header.NumColors)*6) // 3x uint16 per color
		_, err := io.ReadFull(r, skip)
		return err
	case 2: // Bell — no payload
		return nil
	case 3: // ServerCutText
		var pad [3]byte
		if _, err := io.ReadFull(r, pad[:]); err != nil {
			return err
		}
		var textLen uint32
		if err := binary.Read(r, binary.BigEndian, &textLen); err != nil {
			return err
		}
		if textLen > maxRFBNameLength {
			return fmt.Errorf("server cut text is too large: %d bytes", textLen)
		}
		skip := make([]byte, textLen)
		_, err := io.ReadFull(r, skip)
		return err
	default:
		return fmt.Errorf("unknown server message type %d", msgType)
	}
}

// pixelsToPPM converts 32bpp RGBA pixel data to PPM (P6) format.
func pixelsToPPM(width, height uint16, pixels []byte) ([]byte, error) {
	expected := int(width) * int(height) * 4
	if len(pixels) != expected {
		return nil, fmt.Errorf("pixel buffer has %d bytes, want %d", len(pixels), expected)
	}
	header := fmt.Sprintf("P6\n%d %d\n255\n", width, height)
	rgbSize := int(width) * int(height) * 3
	buf := make([]byte, 0, len(header)+rgbSize)
	buf = append(buf, header...)

	// Convert 32bpp (R, G, B, pad) to 24bpp (R, G, B)
	for i := 0; i < len(pixels); i += 4 {
		buf = append(buf, pixels[i], pixels[i+1], pixels[i+2])
	}

	return buf, nil
}

// vncAuthEncrypt performs VNC Authentication DES encryption.
// The password is truncated/padded to 8 bytes, each byte is bit-reversed,
// then used as a DES key to encrypt the 16-byte challenge.
func vncAuthEncrypt(challenge []byte, password string) ([]byte, error) {
	if len(challenge) != 16 {
		return nil, fmt.Errorf("VNC authentication challenge has %d bytes, want 16", len(challenge))
	}
	key := make([]byte, 8)
	for i := 0; i < 8 && i < len(password); i++ {
		key[i] = reverseBits(password[i])
	}

	cipher, err := des.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating VNC authentication cipher: %w", err)
	}

	response := make([]byte, 16)
	cipher.Encrypt(response[0:8], challenge[0:8])
	cipher.Encrypt(response[8:16], challenge[8:16])
	return response, nil
}

// reverseBits reverses the bit order of a byte (VNC DES key quirk).
func reverseBits(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}
