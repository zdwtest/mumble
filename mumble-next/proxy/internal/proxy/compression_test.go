package proxy

import (
	"bytes"
	"compress/flate"
	"testing"
)

func TestCompressionConfig_Default(t *testing.T) {
	cfg := DefaultCompressionConfig()

	if !cfg.Enabled {
		t.Error("expected compression to be enabled by default")
	}
	if cfg.Level != flate.DefaultCompression {
		t.Errorf("expected default compression level, got %d", cfg.Level)
	}
	if cfg.Threshold != 256 {
		t.Errorf("expected threshold 256, got %d", cfg.Threshold)
	}
}

func TestCompressor_ShouldCompress(t *testing.T) {
	cfg := DefaultCompressionConfig()
	cfg.Threshold = 100
	c := NewCompressor(cfg)

	// Small message should not compress
	if c.ShouldCompress(make([]byte, 50)) {
		t.Error("expected small message not to compress")
	}

	// Large message should compress
	if !c.ShouldCompress(make([]byte, 200)) {
		t.Error("expected large message to compress")
	}

	// Disabled
	cfg.Enabled = false
	c = NewCompressor(cfg)
	if c.ShouldCompress(make([]byte, 200)) {
		t.Error("expected no compression when disabled")
	}
}

func TestCompressor_Compress(t *testing.T) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)

	// Test compression
	data := bytes.Repeat([]byte("hello world"), 100)
	compressed, err := c.Compress(data)
	if err != nil {
		t.Fatalf("compress error: %v", err)
	}

	// Compressed should be smaller for repetitive data
	if len(compressed) >= len(data) {
		t.Errorf("expected compression, got %d -> %d", len(data), len(compressed))
	}

	// Decompress and verify
	d := NewDecompressor()
	decompressed, err := d.Decompress(compressed)
	if err != nil {
		t.Fatalf("decompress error: %v", err)
	}

	if !bytes.Equal(data, decompressed) {
		t.Error("decompressed data doesn't match original")
	}
}

func TestCompressor_CompressDisabled(t *testing.T) {
	cfg := DefaultCompressionConfig()
	cfg.Enabled = false
	c := NewCompressor(cfg)

	data := []byte("test data")
	result, err := c.Compress(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(data, result) {
		t.Error("expected original data when disabled")
	}
}

func TestCompressor_SetLevel(t *testing.T) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)

	// Valid levels
	for _, level := range []int{flate.NoCompression, flate.BestSpeed, flate.DefaultCompression, flate.BestCompression} {
		if err := c.SetLevel(level); err != nil {
			t.Errorf("unexpected error for level %d: %v", level, err)
		}
	}

	// Invalid level
	if err := c.SetLevel(-5); err == nil {
		t.Error("expected error for invalid level")
	}
	if err := c.SetLevel(15); err == nil {
		t.Error("expected error for invalid level")
	}
}

func TestCompressor_SetThreshold(t *testing.T) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)

	c.SetThreshold(500)
	if !c.ShouldCompress(make([]byte, 600)) {
		t.Error("expected compression after threshold change")
	}
}

func TestCompressor_IsEnabled(t *testing.T) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)

	if !c.IsEnabled() {
		t.Error("expected to be enabled")
	}

	cfg.Enabled = false
	c = NewCompressor(cfg)
	if c.IsEnabled() {
		t.Error("expected to be disabled")
	}
}

func TestDecompressor_Decompress(t *testing.T) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)
	d := NewDecompressor()

	testCases := [][]byte{
		[]byte("short"),
		bytes.Repeat([]byte("repeat"), 100),
		make([]byte, 1000), // zeros
	}

	for i, data := range testCases {
		compressed, err := c.Compress(data)
		if err != nil {
			t.Fatalf("test %d: compress error: %v", i, err)
		}

		decompressed, err := d.Decompress(compressed)
		if err != nil {
			t.Fatalf("test %d: decompress error: %v", i, err)
		}

		if !bytes.Equal(data, decompressed) {
			t.Errorf("test %d: decompressed data doesn't match", i)
		}
	}
}

func TestCompressionUpgrader_New(t *testing.T) {
	cfg := DefaultCompressionConfig()
	u := NewCompressionUpgrader(cfg)

	if u == nil {
		t.Fatal("expected upgrader to be created")
	}

	upgrader := u.GetUpgrader()
	if upgrader.ReadBufferSize != 4096 {
		t.Errorf("expected read buffer size 4096, got %d", upgrader.ReadBufferSize)
	}
}

func TestCompressionTracker_Record(t *testing.T) {
	tracker := NewCompressionTracker()

	// Record compressed
	tracker.Record(1000, 500, true)
	tracker.Record(2000, 800, true)

	// Record uncompressed
	tracker.Record(100, 100, false)

	stats := tracker.GetStats()

	if stats.TotalCompressed != 2 {
		t.Errorf("expected 2 compressed, got %d", stats.TotalCompressed)
	}
	if stats.TotalUncompressed != 1 {
		t.Errorf("expected 1 uncompressed, got %d", stats.TotalUncompressed)
	}
	if stats.TotalBytesBefore != 3000 {
		t.Errorf("expected 3000 bytes before, got %d", stats.TotalBytesBefore)
	}
	if stats.TotalBytesAfter != 1300 {
		t.Errorf("expected 1300 bytes after, got %d", stats.TotalBytesAfter)
	}
	if stats.AverageRatio < 0.4 || stats.AverageRatio > 0.5 {
		t.Errorf("expected average ratio ~0.43, got %f", stats.AverageRatio)
	}
}

func TestCompressionTracker_Reset(t *testing.T) {
	tracker := NewCompressionTracker()

	tracker.Record(1000, 500, true)
	tracker.Record(100, 100, false)

	stats := tracker.GetStats()
	if stats.TotalCompressed != 1 {
		t.Error("expected records before reset")
	}

	tracker.Reset()

	stats = tracker.GetStats()
	if stats.TotalCompressed != 0 || stats.TotalUncompressed != 0 {
		t.Error("expected zero stats after reset")
	}
}

func TestCompressionError(t *testing.T) {
	err := ErrInvalidCompressionLevel
	if err == nil {
		t.Fatal("expected error to be defined")
	}
	if err.Error() != "invalid compression level" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func BenchmarkCompressor_Compress(b *testing.B) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)
	data := bytes.Repeat([]byte("benchmark test data"), 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Compress(data)
	}
}

func BenchmarkDecompressor_Decompress(b *testing.B) {
	cfg := DefaultCompressionConfig()
	c := NewCompressor(cfg)
	d := NewDecompressor()
	data := bytes.Repeat([]byte("benchmark test data"), 100)
	compressed, _ := c.Compress(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Decompress(compressed)
	}
}