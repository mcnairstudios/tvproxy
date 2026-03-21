package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

var dlnaNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

type DLNAService struct {
	channelRepo     *repository.ChannelRepository
	settingsService *SettingsService
	config          *config.Config
	log             zerolog.Logger
}

func NewDLNAService(
	channelRepo *repository.ChannelRepository,
	settingsService *SettingsService,
	cfg *config.Config,
	log zerolog.Logger,
) *DLNAService {
	return &DLNAService{
		channelRepo:     channelRepo,
		settingsService: settingsService,
		config:          cfg,
		log:             log.With().Str("service", "dlna").Logger(),
	}
}

func (s *DLNAService) IsEnabled(ctx context.Context) bool {
	val, err := s.settingsService.Get(ctx, "dlna_enabled")
	if err != nil {
		return false
	}
	return val == "true"
}

func (s *DLNAService) UDN() string {
	return "uuid:" + uuid.NewSHA1(dlnaNamespace, []byte("tvproxy-dlna")).String()
}

func (s *DLNAService) DeviceDescriptionXML(baseURL string) string {
	udn := s.UDN()
	return `<?xml version="1.0" encoding="UTF-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
    <friendlyName>TVProxy DLNA</friendlyName>
    <manufacturer>TVProxy</manufacturer>
    <modelName>TVProxy</modelName>
    <modelDescription>TVProxy DLNA MediaServer</modelDescription>
    <UDN>` + xmlEscape(udn) + `</UDN>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:ContentDirectory:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId>
        <SCPDURL>/dlna/ContentDirectory.xml</SCPDURL>
        <controlURL>/dlna/control/ContentDirectory</controlURL>
        <eventSubURL></eventSubURL>
      </service>
      <service>
        <serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId>
        <SCPDURL>/dlna/ConnectionManager.xml</SCPDURL>
        <controlURL>/dlna/control/ConnectionManager</controlURL>
        <eventSubURL></eventSubURL>
      </service>
    </serviceList>
  </device>
</root>`
}

const contentDirectorySCPD = `<?xml version="1.0" encoding="UTF-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <actionList>
    <action>
      <name>Browse</name>
      <argumentList>
        <argument><name>ObjectID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
        <argument><name>BrowseFlag</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_BrowseFlag</relatedStateVariable></argument>
        <argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
        <argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
        <argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
        <argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
        <argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
        <argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
      </argumentList>
    </action>
    <action><name>GetSearchCapabilities</name><argumentList>
      <argument><name>SearchCaps</name><direction>out</direction><relatedStateVariable>SearchCapabilities</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetSortCapabilities</name><argumentList>
      <argument><name>SortCaps</name><direction>out</direction><relatedStateVariable>SortCapabilities</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetSystemUpdateID</name><argumentList>
      <argument><name>Id</name><direction>out</direction><relatedStateVariable>SystemUpdateID</relatedStateVariable></argument>
    </argumentList></action>
  </actionList>
  <serviceStateTable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ObjectID</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Result</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_BrowseFlag</name><dataType>string</dataType><allowedValueList><allowedValue>BrowseMetadata</allowedValue><allowedValue>BrowseDirectChildren</allowedValue></allowedValueList></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Filter</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_SortCriteria</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Index</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Count</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_UpdateID</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>SystemUpdateID</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>SearchCapabilities</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>SortCapabilities</name><dataType>string</dataType></stateVariable>
  </serviceStateTable>
</scpd>`

const connectionManagerSCPD = `<?xml version="1.0" encoding="UTF-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <actionList>
    <action><name>GetProtocolInfo</name><argumentList>
      <argument><name>Source</name><direction>out</direction><relatedStateVariable>SourceProtocolInfo</relatedStateVariable></argument>
      <argument><name>Sink</name><direction>out</direction><relatedStateVariable>SinkProtocolInfo</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetCurrentConnectionIDs</name><argumentList>
      <argument><name>ConnectionIDs</name><direction>out</direction><relatedStateVariable>CurrentConnectionIDs</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetCurrentConnectionInfo</name><argumentList>
      <argument><name>ConnectionID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ConnectionID</relatedStateVariable></argument>
      <argument><name>RcsID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_RcsID</relatedStateVariable></argument>
      <argument><name>AVTransportID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_AVTransportID</relatedStateVariable></argument>
      <argument><name>ProtocolInfo</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ProtocolInfo</relatedStateVariable></argument>
      <argument><name>PeerConnectionManager</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionManager</relatedStateVariable></argument>
      <argument><name>PeerConnectionID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionID</relatedStateVariable></argument>
      <argument><name>Direction</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Direction</relatedStateVariable></argument>
      <argument><name>Status</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionStatus</relatedStateVariable></argument>
    </argumentList></action>
  </actionList>
  <serviceStateTable>
    <stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionStatus</name><dataType>string</dataType><allowedValueList><allowedValue>OK</allowedValue><allowedValue>ContentFormatMismatch</allowedValue><allowedValue>InsufficientBandwidth</allowedValue><allowedValue>UnreliableChannel</allowedValue><allowedValue>Unknown</allowedValue></allowedValueList></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionManager</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Direction</name><dataType>string</dataType><allowedValueList><allowedValue>Input</allowedValue><allowedValue>Output</allowedValue></allowedValueList></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionID</name><dataType>i4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_AVTransportID</name><dataType>i4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_RcsID</name><dataType>i4</dataType></stateVariable>
  </serviceStateTable>
</scpd>`

func (s *DLNAService) ContentDirectorySCPD() string {
	return contentDirectorySCPD
}

func (s *DLNAService) ConnectionManagerSCPD() string {
	return connectionManagerSCPD
}

type soapEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    soapBody `xml:"Body"`
}

type soapBody struct {
	Content []byte `xml:",innerxml"`
}

type browseRequest struct {
	ObjectID       string `xml:"ObjectID"`
	BrowseFlag     string `xml:"BrowseFlag"`
	Filter         string `xml:"Filter"`
	StartingIndex  int    `xml:"StartingIndex"`
	RequestedCount int    `xml:"RequestedCount"`
	SortCriteria   string `xml:"SortCriteria"`
}

func (s *DLNAService) HandleContentDirectoryAction(ctx context.Context, baseURL, soapAction string, body []byte) (string, error) {
	action := extractAction(soapAction)

	switch action {
	case "Browse":
		return s.handleBrowse(ctx, baseURL, body)
	case "GetSearchCapabilities":
		return soapResponse("ContentDirectory", "GetSearchCapabilities", "<SearchCaps></SearchCaps>"), nil
	case "GetSortCapabilities":
		return soapResponse("ContentDirectory", "GetSortCapabilities", "<SortCaps></SortCaps>"), nil
	case "GetSystemUpdateID":
		return soapResponse("ContentDirectory", "GetSystemUpdateID", "<Id>1</Id>"), nil
	default:
		return "", fmt.Errorf("unsupported action: %s", action)
	}
}

func (s *DLNAService) HandleConnectionManagerAction(_ context.Context, soapAction string, body []byte) (string, error) {
	action := extractAction(soapAction)

	switch action {
	case "GetProtocolInfo":
		return soapResponse("ConnectionManager", "GetProtocolInfo",
			"<Source>http-get:*:video/mp2t:*,http-get:*:video/mp4:*</Source><Sink></Sink>"), nil
	case "GetCurrentConnectionIDs":
		return soapResponse("ConnectionManager", "GetCurrentConnectionIDs", "<ConnectionIDs>0</ConnectionIDs>"), nil
	case "GetCurrentConnectionInfo":
		return soapResponse("ConnectionManager", "GetCurrentConnectionInfo",
			"<RcsID>-1</RcsID><AVTransportID>-1</AVTransportID><ProtocolInfo></ProtocolInfo>"+
				"<PeerConnectionManager></PeerConnectionManager><PeerConnectionID>-1</PeerConnectionID>"+
				"<Direction>Output</Direction><Status>OK</Status>"), nil
	default:
		return "", fmt.Errorf("unsupported action: %s", action)
	}
}

func (s *DLNAService) handleBrowse(ctx context.Context, baseURL string, body []byte) (string, error) {
	var env soapEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("parsing SOAP envelope: %w", err)
	}

	var req browseRequest
	if err := xml.Unmarshal(env.Body.Content, &req); err != nil {
		return "", fmt.Errorf("parsing Browse request: %w", err)
	}

	switch req.ObjectID {
	case "0":
		return s.browseRoot(req.BrowseFlag)
	case "1":
		return s.browseChannels(ctx, baseURL, req.BrowseFlag, req.StartingIndex, req.RequestedCount)
	default:
		return s.browseChannelItem(ctx, baseURL, req.ObjectID, req.BrowseFlag)
	}
}

func (s *DLNAService) browseRoot(browseFlag string) (string, error) {
	if browseFlag == "BrowseMetadata" {
		didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
			`<container id="0" parentID="-1" childCount="1" restricted="1">` +
			`<dc:title>TVProxy</dc:title><upnp:class>object.container</upnp:class>` +
			`</container></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 1, 1), nil
	}
	didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		`<container id="1" parentID="0" childCount="0" restricted="1">` +
		`<dc:title>All Channels</dc:title><upnp:class>object.container</upnp:class>` +
		`</container></DIDL-Lite>`
	return soapBrowseResponse(xmlEscape(didl), 1, 1), nil
}

func (s *DLNAService) browseChannels(ctx context.Context, baseURL, browseFlag string, startIdx, reqCount int) (string, error) {
	if browseFlag == "BrowseMetadata" {
		channels, err := s.listChannels(ctx)
		if err != nil {
			return "", err
		}
		count := 0
		for _, ch := range channels {
			if ch.IsEnabled {
				count++
			}
		}
		didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
			fmt.Sprintf(`<container id="1" parentID="0" childCount="%d" restricted="1">`, count) +
			`<dc:title>All Channels</dc:title><upnp:class>object.container</upnp:class>` +
			`</container></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 1, 1), nil
	}

	channels, err := s.listChannels(ctx)
	if err != nil {
		return "", fmt.Errorf("listing channels: %w", err)
	}

	type channelEntry struct {
		id   string
		name string
	}
	var enabled []channelEntry
	for _, ch := range channels {
		if ch.IsEnabled {
			enabled = append(enabled, channelEntry{ch.ID, ch.Name})
		}
	}

	total := len(enabled)
	if reqCount <= 0 {
		reqCount = total
	}
	end := startIdx + reqCount
	if end > total {
		end = total
	}
	if startIdx > total {
		startIdx = total
	}
	page := enabled[startIdx:end]

	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)

	for _, ch := range page {
		b.WriteString(fmt.Sprintf(`<item id="ch-%s" parentID="1" restricted="1">`, xmlEscape(ch.id)))
		b.WriteString(fmt.Sprintf(`<dc:title>%s</dc:title>`, xmlEscape(ch.name)))
		b.WriteString(`<upnp:class>object.item.videoItem.videoBroadcast</upnp:class>`)
		b.WriteString(fmt.Sprintf(`<res protocolInfo="http-get:*:video/mp2t:DLNA.ORG_PN=MPEG_TS_SD_EU;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=89000000000000000000000000000000">%s/channel/%s.mp4</res>`,
			xmlEscape(baseURL), xmlEscape(ch.id)))
		b.WriteString(`</item>`)
	}

	b.WriteString(`</DIDL-Lite>`)
	return soapBrowseResponse(xmlEscape(b.String()), len(page), total), nil
}

func (s *DLNAService) browseChannelItem(ctx context.Context, baseURL, objectID, browseFlag string) (string, error) {
	if browseFlag != "BrowseMetadata" {
		didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 0, 0), nil
	}

	channelID := strings.TrimPrefix(objectID, "ch-")
	ch, err := s.getChannel(ctx, channelID)
	if err != nil || ch == nil || !ch.IsEnabled {
		didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 0, 0), nil
	}

	logo := ch.Logo
	if logo == "" {
		logo = placeholderLogo
	}
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
	b.WriteString(fmt.Sprintf(`<item id="ch-%s" parentID="1" restricted="1">`, xmlEscape(ch.ID)))
	b.WriteString(fmt.Sprintf(`<dc:title>%s</dc:title>`, xmlEscape(ch.Name)))
	b.WriteString(`<upnp:class>object.item.videoItem.videoBroadcast</upnp:class>`)
	b.WriteString(fmt.Sprintf(`<upnp:albumArtURI>%s</upnp:albumArtURI>`, xmlEscape(logo)))
	b.WriteString(fmt.Sprintf(`<res protocolInfo="http-get:*:video/mp2t:DLNA.ORG_PN=MPEG_TS_SD_EU;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=89000000000000000000000000000000">%s/channel/%s.mp4</res>`,
		xmlEscape(baseURL), xmlEscape(ch.ID)))
	b.WriteString(`</item></DIDL-Lite>`)
	return soapBrowseResponse(xmlEscape(b.String()), 1, 1), nil
}

func (s *DLNAService) listChannels(ctx context.Context) ([]models.Channel, error) {
	return s.channelRepo.List(ctx)
}

func (s *DLNAService) getChannel(ctx context.Context, id string) (*models.Channel, error) {
	return s.channelRepo.GetByID(ctx, id)
}

func extractAction(soapAction string) string {
	soapAction = strings.Trim(soapAction, "\"")
	if i := strings.LastIndex(soapAction, "#"); i >= 0 {
		return soapAction[i+1:]
	}
	return soapAction
}

func soapResponse(service, action, innerXML string) string {
	ns := "urn:schemas-upnp-org:service:" + service + ":1"
	return `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><u:` + action + `Response xmlns:u="` + ns + `">` +
		innerXML + `</u:` + action + `Response></s:Body></s:Envelope>`
}

func soapBrowseResponse(didlEscaped string, numberReturned, totalMatches int) string {
	return soapResponse("ContentDirectory", "Browse",
		`<Result>`+didlEscaped+`</Result>`+
			`<NumberReturned>`+strconv.Itoa(numberReturned)+`</NumberReturned>`+
			`<TotalMatches>`+strconv.Itoa(totalMatches)+`</TotalMatches>`+
			`<UpdateID>1</UpdateID>`)
}
