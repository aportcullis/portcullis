package crypto

import (
	"crypto/rand"
	"encoding/json"
	"math"
	"strconv"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// ResultCodec encrypts every snapshot chunk with one fresh result key.
type ResultCodec struct{ keyring *Keyring }

// NewResultCodec builds the snapshot codec on the loaded master keys.
func NewResultCodec(keyring *Keyring) *ResultCodec { return &ResultCodec{keyring: keyring} }

// Seal authenticates encrypted column metadata and row chunks by result identity.
func (c *ResultCodec) Seal(metadata query.SnapshotMetadata, columns []query.Column, rows [][]query.CellValue) (query.SealedResult, error) {
	return c.SealWithinBudget(metadata, columns, rows, math.MaxInt64)
}

// SealWithinBudget encrypts the longest row prefix whose sealed size fits limitBytes, measuring each row once before a single encryption pass. RowCount reports the sealed prefix; a schema that alone exceeds the budget is sealed with no rows and a ByteCount above the limit for the caller to refuse.
func (c *ResultCodec) SealWithinBudget(metadata query.SnapshotMetadata, columns []query.Column, rows [][]query.CellValue, limitBytes int64) (query.SealedResult, error) {
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
	gcm, err := newGCM(key)
	if err != nil {
		return query.SealedResult{}, err
	}
	chunkOverhead := int64(gcmNonceLen + gcm.Overhead())
	columnsPlaintext, err := json.Marshal(columns)
	if err != nil {
		return query.SealedResult{}, err
	}
	sealed := query.SealedResult{Metadata: metadata, KeyVersion: uint32(c.keyring.active), WrappedDEK: append(nonce, wrapped...)}
	plannedBytes := int64(len(sealed.WrappedDEK)) + chunkOverhead + int64(len(columnsPlaintext))
	encodedRows := make([][]byte, 0, min(len(rows), query.ResultChunkRows))
	for idx, row := range rows {
		encoded, err := json.Marshal(canonicalChunkRow(row))
		if err != nil {
			return query.SealedResult{}, err
		}
		// A chunk's plaintext is the JSON array of its rows: the first row adds the chunk overhead and brackets, later rows a comma.
		rowBytes := int64(len(encoded)) + 1
		if idx%query.ResultChunkRows == 0 {
			rowBytes = int64(len(encoded)) + 2 + chunkOverhead
		}
		if plannedBytes+rowBytes > limitBytes {
			break
		}
		plannedBytes += rowBytes
		encodedRows = append(encodedRows, encoded)
	}
	sealed.Metadata.RowCount = int64(len(encodedRows))
	sealChunk := func(index int, plaintext []byte) error {
		nonce, ciphertext, err := sealAEAD(key, plaintext, resultChunkAAD(metadata, index))
		if err != nil {
			return err
		}
		sealed.Chunks = append(sealed.Chunks, query.SealedResultChunk{Index: index, Nonce: nonce, Ciphertext: ciphertext})
		sealed.Metadata.ByteCount += int64(len(nonce) + len(ciphertext))
		return nil
	}
	sealed.Metadata.ByteCount = int64(len(sealed.WrappedDEK))
	if err := sealChunk(0, columnsPlaintext); err != nil {
		return query.SealedResult{}, err
	}
	for offset := 0; offset < len(encodedRows); offset += query.ResultChunkRows {
		end := min(offset+query.ResultChunkRows, len(encodedRows))
		if err := sealChunk(offset/query.ResultChunkRows+1, joinJSONArray(encodedRows[offset:end])); err != nil {
			return query.SealedResult{}, err
		}
	}
	return sealed, nil
}

// canonicalChunkRow copies a row with floats rendered as text, so non-finite values survive JSON.
func canonicalChunkRow(row []query.CellValue) []query.CellValue {
	copied := append([]query.CellValue(nil), row...)
	for cellIdx := range copied {
		cell := &copied[cellIdx]
		if cell.Kind == query.CellFloat {
			cell.Text = strconv.FormatFloat(cell.Float, 'g', -1, 64)
			cell.Float = 0
		}
	}
	return copied
}

// joinJSONArray concatenates encoded elements into the exact bytes json.Marshal produces for their slice.
func joinJSONArray(elements [][]byte) []byte {
	size := 2
	for _, element := range elements {
		size += len(element) + 1
	}
	joined := make([]byte, 0, size)
	joined = append(joined, '[')
	for idx, element := range elements {
		if idx > 0 {
			joined = append(joined, ',')
		}
		joined = append(joined, element...)
	}
	return append(joined, ']')
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
