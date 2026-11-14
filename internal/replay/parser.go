package replay

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseReplayFile reads and parses the header of a Rocket League .replay file from disk.
func ParseReplayFile(filePath string) (*ReplayMetadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("open replay: %w", err)
	}
	return ParseReplay(bytes.NewReader(data))
}

// ParseReplay reads and parses the header properties from an io.ReadSeeker.
func ParseReplay(r io.ReadSeeker) (*ReplayMetadata, error) {
	br, err := newBoundedReader(r)
	if err != nil {
		return nil, fmt.Errorf("init replay reader: %w", err)
	}
	if err := parseHeader(br); err != nil {
		return nil, err
	}

	props, err := parsePropertyMap(br)
	if err != nil {
		return nil, fmt.Errorf("parse property map: %w", err)
	}

	meta := &ReplayMetadata{}
	populateMetadata(meta, props)

	if meta.Team0Score > meta.Team1Score {
		w := 0
		meta.WinnerTeam = &w
	} else if meta.Team1Score > meta.Team0Score {
		w := 1
		meta.WinnerTeam = &w
	}
	return meta, nil
}

func parseHeader(br *boundedReader) error {
	headerLen, err := br.readInt32()
	if err != nil {
		return fmt.Errorf("read headerLen: %w", err)
	}
	if headerLen <= 0 || headerLen > maxHeaderLen {
		return fmt.Errorf("invalid headerLen: %d", headerLen)
	}
	if _, err := br.readInt32(); err != nil {
		return fmt.Errorf("read crc: %w", err)
	}
	engVer, err := br.readInt32()
	if err != nil {
		return fmt.Errorf("read engineVersion: %w", err)
	}
	licVer, err := br.readInt32()
	if err != nil {
		return fmt.Errorf("read licenseeVersion: %w", err)
	}
	if engVer >= netVersionEngineThreshold && licVer >= netVersionLicenseeThreshold {
		if _, err := br.readInt32(); err != nil {
			return fmt.Errorf("read netVersion: %w", err)
		}
	}

	className, err := br.readFString()
	if err != nil {
		return fmt.Errorf("read className: %w", err)
	}
	if !strings.HasPrefix(className, "TAGame.") {
		return fmt.Errorf("unrecognized replay class %q", className)
	}
	return nil
}

func parsePropertyMap(br *boundedReader) (map[string]*propertyValue, error) {
	return parsePropertyMapWithDepth(br, 0)
}

func parsePropertyMapWithDepth(br *boundedReader, depth int) (map[string]*propertyValue, error) {
	if depth > maxPropertyDepth {
		return nil, fmt.Errorf("property nesting depth %d exceeds max %d", depth, maxPropertyDepth)
	}
	props := make(map[string]*propertyValue)

	for {
		name, err := br.readFString()
		if err != nil {
			return nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "None" {
			break
		}

		typeName, err := br.readFString()
		if err != nil {
			return nil, err
		}
		typeName = strings.TrimSpace(typeName)

		dataSize, err := br.readInt64()
		if err != nil {
			return nil, err
		}
		if dataSize < 0 || dataSize > maxPropertyDataSize {
			return nil, fmt.Errorf("invalid property dataSize %d", dataSize)
		}

		pv, err := parsePropertyValue(br, name, typeName, dataSize, depth)
		if err != nil {
			return nil, err
		}
		props[name] = pv
	}

	return props, nil
}

func parsePropertyValue(br *boundedReader, name, typeName string, dataSize int64, depth int) (*propertyValue, error) {
	pv := &propertyValue{typeName: typeName}

	switch typeName {
	case "IntProperty":
		v, err := br.readInt32()
		if err != nil {
			return nil, err
		}
		pv.intVal = int(v)

	case "StrProperty", "NameProperty":
		s, err := br.readFString()
		if err != nil {
			return nil, err
		}
		pv.strVal = s

	case "FloatProperty":
		v, err := br.readFloat32()
		if err != nil {
			return nil, err
		}
		pv.floatVal = v

	case "BoolProperty":
		b, err := br.readBool()
		if err != nil {
			return nil, err
		}
		pv.boolVal = b

	case "QWordProperty":
		v, err := br.readInt64()
		if err != nil {
			return nil, err
		}
		pv.int64Val = v

	case "ByteProperty":
		strVal, intVal, err := br.readBytePropertyValue(dataSize)
		if err != nil {
			return nil, err
		}
		pv.strVal = strVal
		pv.intVal = intVal

	case "ArrayProperty":
		count, err := br.readInt32()
		if err != nil {
			return nil, err
		}
		if count < 0 || count > maxArrayCount {
			return nil, fmt.Errorf("invalid array count %d", count)
		}
		if name == "PlayerStats" {
			const minPropMapBytes = 4
			if int64(count)*minPropMapBytes > br.remaining() {
				return nil, fmt.Errorf("player stats count %d exceeds remaining %d: %w", count, br.remaining(), io.ErrUnexpectedEOF)
			}
			for i := int32(0); i < count; i++ {
				sub, subErr := parsePropertyMapWithDepth(br, depth+1)
				if subErr != nil {
					return nil, subErr
				}
				pv.subProps = append(pv.subProps, sub)
			}
		} else if err := br.skipArrayPropertyPayload(dataSize); err != nil {
			return nil, err
		}

	case "StructProperty":
		structType, err := br.readFString()
		if err != nil {
			return nil, err
		}
		if structType == "UniqueNetId" {
			var innerProps map[string]*propertyValue
			var innerErr error
			if err := br.withBound(dataSize, func() error {
				innerProps, innerErr = parsePropertyMapWithDepth(br, depth+1)
				return nil
			}); err != nil {
				return nil, err
			}
			if innerErr == nil {
				pv.subProps = []map[string]*propertyValue{innerProps}
			}
		} else if err := br.skipPropertyPayload(dataSize); err != nil {
			return nil, err
		}

	default:
		if err := br.skipPropertyPayload(dataSize); err != nil {
			return nil, err
		}
	}

	return pv, nil
}
