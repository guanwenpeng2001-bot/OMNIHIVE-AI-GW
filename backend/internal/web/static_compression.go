//go:build embed || unit

package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

type staticRepresentation struct {
	body     []byte
	encoding string
	etag     string
}

type staticResponseCache struct {
	identity staticRepresentation
	gzip     staticRepresentation
	brotli   staticRepresentation
}

func newStaticResponseCache(content []byte) staticResponseCache {
	cache := staticResponseCache{identity: staticRepresentation{body: content, etag: staticETag(content, "")}}
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err == nil {
		_, err = writer.Write(content)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		cache.gzip = staticRepresentation{body: bytes.Clone(compressed.Bytes()), encoding: "gzip", etag: staticETag(content, "gzip")}
	}
	compressed.Reset()
	brotliWriter := brotli.NewWriterLevel(&compressed, brotli.BestSpeed)
	_, err = brotliWriter.Write(content)
	if closeErr := brotliWriter.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		cache.brotli = staticRepresentation{body: compressed.Bytes(), encoding: "br", etag: staticETag(content, "br")}
	}
	return cache
}

func staticETag(content []byte, encoding string) string {
	digest := sha256.Sum256(append([]byte(encoding+"\x00"), content...))
	return fmt.Sprintf(`"%x"`, digest[:16])
}

func selectStaticRepresentation(cache staticResponseCache, acceptEncoding string) staticRepresentation {
	for _, encoding := range strings.Split(acceptEncoding, ",") {
		name, parameters, _ := strings.Cut(strings.TrimSpace(encoding), ";")
		if strings.Contains(strings.ReplaceAll(parameters, " ", ""), "q=0") {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "br":
			if cache.brotli.body != nil {
				return cache.brotli
			}
		case "gzip":
			if cache.gzip.body != nil {
				return cache.gzip
			}
		}
	}
	return cache.identity
}

func serveStaticRepresentation(c *gin.Context, cache staticResponseCache, contentType string) {
	representation := selectStaticRepresentation(cache, c.GetHeader("Accept-Encoding"))
	header := c.Writer.Header()
	header.Set("Content-Type", contentType)
	header.Add("Vary", "Accept-Encoding")
	header.Set("ETag", representation.etag)
	header.Set("Content-Length", strconv.Itoa(len(representation.body)))
	if representation.encoding != "" {
		header.Set("Content-Encoding", representation.encoding)
	}
	if match := c.GetHeader("If-None-Match"); match != "" && etagMatches(match, representation.etag) {
		c.Status(http.StatusNotModified)
		c.Abort()
		return
	}
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		c.Abort()
		return
	}
	c.Data(http.StatusOK, contentType, representation.body)
	c.Abort()
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/")) == etag {
			return true
		}
	}
	return false
}
