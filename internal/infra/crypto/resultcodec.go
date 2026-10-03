package crypto

import (
	"crypto/rand"
	"encoding/json"
	"strconv"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// ResultCodec encrypts every snapshot chunk with one fresh result key.
type ResultCodec struct{ keyring *Keyring }

// NewResultCodec builds the snapshot codec on the loaded master keys.
func NewResultCodec(keyring *Keyring) *ResultCodec { return &ResultCodec{keyring: keyring} }

// Seal authenticates encrypted column metadata and row chunks by result identity.
func (c *ResultCodec) Seal(metadata query.SnapshotMetadata, columns []query.Column, rows [][]query.CellValue) (query.SealedResult, error) {
	key := make([]byte, dekLen)
	if _, err := rand.Read(key); err != nil {
		return query.SealedResult{}, err
	}
	wrapKey, err := c.keyring.derive(c.keyring.active, infoDEKWrap)
	if err != nil {
		return query.SealedResult{}, err
	}
	aad := AAD("result_set", string(metadata.OrganizationID), metadata.ID)
	nonce, wrapped, err := sealAEAD(wrapKey, key, aad)
	if err != nil {
		return query.SealedResult{}, err
	}
	sealed := query.SealedResult{Metadata: metadata, KeyVersion: uint32(c.keyring.active), WrappedDEK: append(nonce, wrapped...)}
	sealChunk := func(index int, value any) error {
		plaintext, err := json.Marshal(value)
		if err != nil {
			return err
		}
		nonce, ciphertext, err := sealAEAD(key, plaintext, resultChunkAAD(metadata, index))
		if err != nil {
			return err
		}
		sealed.Chunks = append(sealed.Chunks, query.SealedResultChunk{Index: index, Nonce: nonce, Ciphertext: ciphertext})
		sealed.Metadata.ByteCount += int64(len(nonce) + len(ciphertext))
		return nil
	}
	sealed.Metadata.ByteCount = int64(len(sealed.WrappedDEK))
	if err := sealChunk(0, columns); err != nil {
		return query.SealedResult{}, err
	}
	for offset := 0; offset < len(rows); offset += query.ResultChunkRows {
		end := min(offset+query.ResultChunkRows, len(rows))
		chunkRows := make([][]query.CellValue, end-offset)
		for idx, row := range rows[offset:end] {
			chunkRows[idx] = append([]query.CellValue(nil), row...)
			for cellIdx := range chunkRows[idx] {
				cell := &chunkRows[idx][cellIdx]
				if cell.Kind == query.CellFloat {
					cell.Text = strconv.FormatFloat(cell.Float, 'g', -1, 64)
					cell.Float = 0
				}
			}
		}
		if err := sealChunk(offset/query.ResultChunkRows+1, chunkRows); err != nil {
			return query.SealedResult{}, err
		}
	}
	return sealed, nil
}

// OpenColumns decrypts the snapshot's schema chunk.
func (c *ResultCodec) OpenColumns(result query.SealedResult, chunk query.SealedResultChunk) ([]query.Column, error) {
	if chunk.Index != 0 {
		return nil, ErrDecrypt
	}
	plaintext, err := c.openChunk(result, chunk)
	if err != nil {
		return nil, err
	}
	var columns []query.Column
	if err := json.Unmarshal(plaintext, &columns); err != nil {
		return nil, ErrDecrypt
	}
	return columns, nil
}

// OpenRows decrypts one snapshot row range.
func (c *ResultCodec) OpenRows(result query.SealedResult, chunk query.SealedResultChunk) ([][]query.CellValue, error) {
	if chunk.Index < 1 {
		return nil, ErrDecrypt
	}
	plaintext, err := c.openChunk(result, chunk)
	if err != nil {
		return nil, err
	}
	var rows [][]query.CellValue
	if err := json.Unmarshal(plaintext, &rows); err != nil {
		return nil, ErrDecrypt
	}
	for rowIdx := range rows {
		for cellIdx := range rows[rowIdx] {
			cell := &rows[rowIdx][cellIdx]
			if cell.Kind == query.CellFloat {
				cell.Float, err = strconv.ParseFloat(cell.Text, 64)
				if err != nil {
					return nil, ErrDecrypt
				}
				cell.Text = ""
			}
		}
	}
	return rows, nil
}

func resultChunkAAD(metadata query.SnapshotMetadata, index int) []byte {
	return append(AAD("result_chunk", string(metadata.OrganizationID), metadata.ID), []byte("|"+strconv.Itoa(index))...)
}

func (c *ResultCodec) openChunk(result query.SealedResult, chunk query.SealedResultChunk) ([]byte, error) {
	wrapKey, err := c.keyring.derive(KeyVersion(result.KeyVersion), infoDEKWrap)
	if err != nil {
		return nil, err
	}
	if len(result.WrappedDEK) < gcmNonceLen {
		return nil, ErrDecrypt
	}
	key, err := openAEAD(wrapKey, result.WrappedDEK[:gcmNonceLen], result.WrappedDEK[gcmNonceLen:], AAD("result_set", string(result.Metadata.OrganizationID), result.Metadata.ID))
	if err != nil {
		return nil, err
	}
	return openAEAD(key, chunk.Nonce, chunk.Ciphertext, resultChunkAAD(result.Metadata, chunk.Index))
}
