package pdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"fmt"
	"io"
)

var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func (r *Reader) initEncrypt(password string) error {
	// See PDF 32000-1:2008, §7.6.
	encrypt, _ := r.resolve(objptr{}, r.trailer["Encrypt"]).data.(dict)
	if encrypt["Filter"] != name("Standard") {
		return fmt.Errorf("unsupported PDF: encryption filter %v", objfmt(encrypt["Filter"]))
	}
	n, _ := encrypt["Length"].(int64)
	if n == 0 {
		n = 40
	}
	if n%8 != 0 || n > 128 || n < 40 {
		return fmt.Errorf("malformed PDF: %d-bit encryption key", n)
	}
	V, _ := encrypt["V"].(int64)
	if V != 1 && V != 2 && (V != 4 || !okayV4(encrypt)) {
		return fmt.Errorf("unsupported PDF: encryption version V=%d; %v", V, objfmt(encrypt))
	}
	if V == 4 && encrypt["StmF"].(name) == name("Identity") {
		return nil // No encryption applied.
	}

	ids, ok := r.trailer["ID"].(array)
	if !ok || len(ids) < 1 {
		return fmt.Errorf("malformed PDF: missing ID in trailer")
	}
	idstr, ok := ids[0].(string)
	if !ok {
		return fmt.Errorf("malformed PDF: missing ID in trailer")
	}
	ID := []byte(idstr)

	R, _ := encrypt["R"].(int64)
	if R < 2 {
		return fmt.Errorf("malformed PDF: encryption revision R=%d", R)
	}
	if R > 4 {
		return fmt.Errorf("unsupported PDF: encryption revision R=%d", R)
	}
	O, _ := encrypt["O"].(string)
	U, _ := encrypt["U"].(string)
	if len(O) != 32 || len(U) != 32 {
		return fmt.Errorf("malformed PDF: missing O= or U= encryption parameters")
	}
	p, _ := encrypt["P"].(int64)
	P := uint32(p)

	// TODO: Password should be converted to Latin-1.
	pw := []byte(password)
	h := md5.New()
	if len(pw) >= 32 {
		h.Write(pw[:32])
	} else {
		h.Write(pw)
		h.Write(passwordPad[:32-len(pw)])
	}
	h.Write([]byte(O))
	h.Write([]byte{byte(P), byte(P >> 8), byte(P >> 16), byte(P >> 24)})
	h.Write([]byte(ID))
	key := h.Sum(nil)

	if R >= 3 {
		for i := 0; i < 50; i++ {
			h.Reset()
			h.Write(key[:n/8])
			key = h.Sum(key[:0])
		}
		key = key[:n/8]
	} else {
		key = key[:40/8]
	}

	c, err := rc4.NewCipher(key)
	if err != nil {
		return fmt.Errorf("malformed PDF: invalid RC4 key: %v", err)
	}

	var u []byte
	if R == 2 {
		u = make([]byte, 32)
		copy(u, passwordPad)
		c.XORKeyStream(u, u)
	} else {
		h.Reset()
		h.Write(passwordPad)
		h.Write([]byte(ID))
		u = h.Sum(nil)
		c.XORKeyStream(u, u)

		for i := 1; i <= 19; i++ {
			key1 := make([]byte, len(key))
			copy(key1, key)
			for j := range key1 {
				key1[j] ^= byte(i)
			}
			c, _ = rc4.NewCipher(key1)
			c.XORKeyStream(u, u)
		}
	}

	if !bytes.HasPrefix([]byte(U), u) {
		return ErrInvalidPassword
	}

	r.key = key
	r.useAES = V == 4

	return nil
}

var ErrInvalidPassword = fmt.Errorf("encrypted PDF: invalid password")

func okayV4(encrypt dict) bool {
	cf, ok := encrypt["CF"].(dict)
	if !ok {
		return false
	}
	stmf, ok := encrypt["StmF"].(name)
	if !ok {
		return false
	}
	strf, ok := encrypt["StrF"].(name)
	if !ok {
		return false
	}
	if stmf != strf {
		return false
	}
	if stmf == name("Identity") {
		return true // No encryption applied.
	}
	cfparam, ok := cf[stmf].(dict)
	if cfparam["AuthEvent"] != nil && cfparam["AuthEvent"] != name("DocOpen") {
		return false
	}
	if cfparam["Length"] != nil && cfparam["Length"] != int64(16) {
		return false
	}
	if cfparam["CFM"] != name("AESV2") {
		return false
	}
	return true
}

func cryptKey(key []byte, useAES bool, ptr objptr) []byte {
	h := md5.New()
	h.Write(key)
	h.Write([]byte{byte(ptr.id), byte(ptr.id >> 8), byte(ptr.id >> 16), byte(ptr.gen), byte(ptr.gen >> 8)})
	if useAES {
		h.Write([]byte("sAlT"))
	}
	return h.Sum(nil)
}

func decryptString(key []byte, useAES bool, ptr objptr, x string) string {
	key = cryptKey(key, useAES, ptr)
	if useAES {
		panic("AES not implemented")
	} else {
		c, _ := rc4.NewCipher(key)
		data := []byte(x)
		c.XORKeyStream(data, data)
		x = string(data)
	}
	return x
}

func decryptStream(key []byte, useAES bool, ptr objptr, rd io.Reader) io.Reader {
	key = cryptKey(key, useAES, ptr)
	if useAES {
		cb, err := aes.NewCipher(key)
		if err != nil {
			panic("AES: " + err.Error())
		}
		iv := make([]byte, 16)
		io.ReadFull(rd, iv)
		cbc := cipher.NewCBCDecrypter(cb, iv)
		rd = &cbcReader{cbc: cbc, rd: rd, buf: make([]byte, 16)}
	} else {
		c, _ := rc4.NewCipher(key)
		rd = &cipher.StreamReader{S: c, R: rd}
	}
	return rd
}

type cbcReader struct {
	cbc  cipher.BlockMode
	rd   io.Reader
	buf  []byte
	pend []byte
}

func (r *cbcReader) Read(b []byte) (n int, err error) {
	if len(r.pend) == 0 {
		_, err = io.ReadFull(r.rd, r.buf)
		if err != nil {
			return 0, err
		}
		r.cbc.CryptBlocks(r.buf, r.buf)
		r.pend = r.buf
	}
	n = copy(b, r.pend)
	r.pend = r.pend[n:]
	return n, nil
}
