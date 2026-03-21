package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

var dlnaNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

const didlHeader = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/">`

type DLNAService struct {
	channelRepo      *repository.ChannelRepository
	channelGroupRepo *repository.ChannelGroupRepository
	settingsService  *SettingsService
	logoService      *LogoService
	config           *config.Config
	log              zerolog.Logger
}

func NewDLNAService(
	channelRepo *repository.ChannelRepository,
	channelGroupRepo *repository.ChannelGroupRepository,
	settingsService *SettingsService,
	logoService *LogoService,
	cfg *config.Config,
	log zerolog.Logger,
) *DLNAService {
	return &DLNAService{
		channelRepo:      channelRepo,
		channelGroupRepo: channelGroupRepo,
		settingsService:  settingsService,
		logoService:      logoService,
		config:           cfg,
		log:              log.With().Str("service", "dlna").Logger(),
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

	switch {
	case req.ObjectID == "0":
		return s.browseRoot(ctx, req.BrowseFlag)
	case strings.HasPrefix(req.ObjectID, "grp-"):
		return s.browseGroup(ctx, baseURL, req.ObjectID, req.BrowseFlag, req.StartingIndex, req.RequestedCount)
	case strings.HasPrefix(req.ObjectID, "ch-"):
		return s.browseChannelItem(ctx, baseURL, req.ObjectID, req.BrowseFlag)
	default:
		didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 0, 0), nil
	}
}

func (s *DLNAService) browseRoot(ctx context.Context, browseFlag string) (string, error) {
	groups, ungroupedCount, err := s.groupedChannelCounts(ctx)
	if err != nil {
		return "", err
	}

	childCount := len(groups)
	if ungroupedCount > 0 {
		childCount++
	}

	if browseFlag == "BrowseMetadata" {
		didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
			fmt.Sprintf(`<container id="0" parentID="-1" childCount="%d" restricted="1">`, childCount) +
			`<dc:title>TVProxy</dc:title><upnp:class>object.container</upnp:class>` +
			`</container></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 1, 1), nil
	}

	var b strings.Builder
	b.WriteString(didlHeader)
	for _, g := range groups {
		b.WriteString(fmt.Sprintf(`<container id="grp-%s" parentID="0" childCount="%d" restricted="1">`,
			xmlEscape(g.id), g.count))
		b.WriteString(fmt.Sprintf(`<dc:title>%s</dc:title>`, xmlEscape(g.name)))
		b.WriteString(`<upnp:class>object.container</upnp:class></container>`)
	}
	if ungroupedCount > 0 {
		b.WriteString(fmt.Sprintf(`<container id="grp-ungrouped" parentID="0" childCount="%d" restricted="1">`,
			ungroupedCount))
		b.WriteString(`<dc:title>Ungrouped</dc:title><upnp:class>object.container</upnp:class></container>`)
	}
	b.WriteString(`</DIDL-Lite>`)
	return soapBrowseResponse(xmlEscape(b.String()), childCount, childCount), nil
}

func (s *DLNAService) browseGroup(ctx context.Context, baseURL, objectID, browseFlag string, startIdx, reqCount int) (string, error) {
	groupID := strings.TrimPrefix(objectID, "grp-")

	channels, err := s.listChannels(ctx)
	if err != nil {
		return "", err
	}

	var enabled []channelEntry
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if groupID == "ungrouped" {
			if ch.ChannelGroupID == nil {
				enabled = append(enabled, channelEntry{ch.ID, ch.Name, s.logoService.ResolveChannel(ch)})
			}
		} else {
			if ch.ChannelGroupID != nil && *ch.ChannelGroupID == groupID {
				enabled = append(enabled, channelEntry{ch.ID, ch.Name, s.logoService.ResolveChannel(ch)})
			}
		}
	}

	if browseFlag == "BrowseMetadata" {
		title := "Ungrouped"
		if groupID != "ungrouped" {
			group, err := s.channelGroupRepo.GetByID(ctx, groupID)
			if err == nil {
				title = group.Name
			}
		}
		didl := didlHeader +
			fmt.Sprintf(`<container id="%s" parentID="0" childCount="%d" restricted="1">`, xmlEscape(objectID), len(enabled)) +
			fmt.Sprintf(`<dc:title>%s</dc:title>`, xmlEscape(title)) +
			`<upnp:class>object.container</upnp:class></container></DIDL-Lite>`
		return soapBrowseResponse(xmlEscape(didl), 1, 1), nil
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
	b.WriteString(didlHeader)
	for _, ch := range page {
		b.WriteString(fmt.Sprintf(`<item id="ch-%s" parentID="%s" restricted="1">`, xmlEscape(ch.id), xmlEscape(objectID)))
		b.WriteString(fmt.Sprintf(`<dc:title>%s</dc:title>`, xmlEscape(ch.name)))
		b.WriteString(`<upnp:class>object.item.videoItem.videoBroadcast</upnp:class>`)
		if ch.logo != "" && strings.HasPrefix(ch.logo, "http") {
			profile, mime := dlnaLogoMeta(ch.logo)
			b.WriteString(fmt.Sprintf(`<upnp:albumArtURI dlna:profileID="%s">%s</upnp:albumArtURI>`, profile, xmlEscape(ch.logo)))
			b.WriteString(fmt.Sprintf(`<res protocolInfo="http-get:*:%s:DLNA.ORG_PN=%s;DLNA.ORG_FLAGS=00f00000000000000000000000000000">%s</res>`, mime, profile, xmlEscape(ch.logo)))
		}
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

	parentID := "grp-ungrouped"
	if ch.ChannelGroupID != nil {
		parentID = "grp-" + *ch.ChannelGroupID
	}

	logo := s.logoService.ResolveChannel(*ch)
	var b strings.Builder
	b.WriteString(didlHeader)
	b.WriteString(fmt.Sprintf(`<item id="ch-%s" parentID="%s" restricted="1">`, xmlEscape(ch.ID), xmlEscape(parentID)))
	b.WriteString(fmt.Sprintf(`<dc:title>%s</dc:title>`, xmlEscape(ch.Name)))
	b.WriteString(`<upnp:class>object.item.videoItem.videoBroadcast</upnp:class>`)
	if strings.HasPrefix(logo, "http") {
		profile, mime := dlnaLogoMeta(logo)
		b.WriteString(fmt.Sprintf(`<upnp:albumArtURI dlna:profileID="%s">%s</upnp:albumArtURI>`, profile, xmlEscape(logo)))
		b.WriteString(fmt.Sprintf(`<res protocolInfo="http-get:*:%s:DLNA.ORG_PN=%s;DLNA.ORG_FLAGS=00f00000000000000000000000000000">%s</res>`, mime, profile, xmlEscape(logo)))
	}
	b.WriteString(fmt.Sprintf(`<res protocolInfo="http-get:*:video/mp2t:DLNA.ORG_PN=MPEG_TS_SD_EU;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=89000000000000000000000000000000">%s/channel/%s.mp4</res>`,
		xmlEscape(baseURL), xmlEscape(ch.ID)))
	b.WriteString(`</item></DIDL-Lite>`)
	return soapBrowseResponse(xmlEscape(b.String()), 1, 1), nil
}

type channelEntry struct {
	id   string
	name string
	logo string
}

func dlnaLogoMeta(logoURL string) (profileID, mimeType string) {
	ext := strings.ToLower(filepath.Ext(strings.SplitN(logoURL, "?", 2)[0]))
	switch ext {
	case ".png":
		return "PNG_SM", "image/png"
	case ".gif":
		return "GIF_LG", "image/gif"
	default:
		return "JPEG_SM", "image/jpeg"
	}
}

type groupCount struct {
	id    string
	name  string
	count int
}

func (s *DLNAService) groupedChannelCounts(ctx context.Context) ([]groupCount, int, error) {
	channels, err := s.listChannels(ctx)
	if err != nil {
		return nil, 0, err
	}
	groups, err := s.channelGroupRepo.List(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("listing channel groups: %w", err)
	}

	counts := make(map[string]int)
	ungrouped := 0
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if ch.ChannelGroupID == nil {
			ungrouped++
		} else {
			counts[*ch.ChannelGroupID]++
		}
	}

	var result []groupCount
	for _, g := range groups {
		if c := counts[g.ID]; c > 0 {
			result = append(result, groupCount{g.ID, g.Name, c})
		}
	}
	return result, ungrouped, nil
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
