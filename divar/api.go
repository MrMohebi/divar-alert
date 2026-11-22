package divar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type APIPath struct {
	SearchList string
	PostDetail string
}

// PostDetailRes represents the response structure for a single post detail
type PostDetailRes struct {
	Sections  []Section `json:"sections"`
	Share     Share     `json:"share"`
	Seo       Seo       `json:"seo"`
	Contact   Contact   `json:"contact"`
	Webengage Webengage `json:"webengage"`
	Analytics Analytics `json:"analytics"`
	City      City      `json:"city"`
}

type City struct {
	CityID     string `json:"city_id"`
	Name       string `json:"name"`
	ParentID   string `json:"parent_id"`
	IsProvince bool   `json:"is_province"`
	SecondSlug string `json:"second_slug"`
}

type Analytics struct {
	Cat1 string `json:"cat1"`
	Cat2 string `json:"cat2"`
	Cat3 string `json:"cat3"`
	City string `json:"city"`
}

type Webengage struct {
	Originality  string `json:"originality"`
	City         string `json:"city"`
	Cat3         string `json:"cat_3"`
	BusinessRef  string `json:"business_ref"`
	Token        string `json:"token"`
	ImageCount   int    `json:"image_count"`
	Category     string `json:"category"`
	Rent         int    `json:"rent"`
	SourceView   string `json:"source_view"`
	BusinessType string `json:"business_type"`
	Price        int    `json:"price"`
	Credit       int    `json:"credit"`
	District     string `json:"district"`
	BrandModel   string `json:"brand_model"`
	Gender       string `json:"gender"`
	Cat2         string `json:"cat_2"`
	Status       string `json:"status"`
	Cat1         string `json:"cat_1"`
}

type Contact struct {
	ChatEnabled                  bool       `json:"chat_enabled"`
	ApplyEnabled                 bool       `json:"apply_enabled"`
	ContactEncryptedData         string     `json:"contact_encrypted_data"`
	SecureCallEnabled            bool       `json:"secure_call_enabled"`
	ExperimentalAPIEnabled       bool       `json:"experimental_api_enabled"`
	ActionLog                    ActionLog2 `json:"action_log"`
	ContactUUID                  string     `json:"contact_uuid"`
	TrackerSessionID             string     `json:"tracker_session_id"`
	SupplierChatAssistantEnabled bool       `json:"supplier_chat_assistant_enabled"`
	EshopEnabled                 bool       `json:"eshop_enabled"`
}

type ActionLog2 struct {
	ServerSideInfo ServerSideInfo2 `json:"server_side_info"`
	Enabled        bool            `json:"enabled"`
}

type ServerSideInfo2 struct {
	Info     Info     `json:"info"`
	ItemType ItemType `json:"item_type"`
}

type Info struct {
	Type                     string `json:"@type"`
	PostToken                string `json:"post_token"`
	SourceView               string `json:"source_view"`
	EventID                  string `json:"event_id"`
	MethodName               string `json:"method_name"`
	ExperimentalAPIEnabled   bool   `json:"experimental_api_enabled"`
	ContactUUID              string `json:"contact_uuid"`
	ContactLayerExperimentID int    `json:"contact_layer_experiment_id"`
	TrackerSessionID         string `json:"tracker_session_id"`
	CallMethod               string `json:"call_method"`
}

type Seo struct {
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AndroidPackageName string          `json:"android_package_name"`
	AndroidAppURL      string          `json:"android_app_url"`
	WebInfo            WebInfo         `json:"web_info"`
	UnavailableAfter   string          `json:"unavailable_after"`
	BreadCrumb         json.RawMessage `json:"bread_crumb"`
}

type WebInfo struct {
	Title               string `json:"title"`
	DistrictPersian     string `json:"district_persian"`
	CityPersian         string `json:"city_persian"`
	CategorySlugPersian string `json:"category_slug_persian"`
}

type Share struct {
	Title    string `json:"title"`
	WebURL   string `json:"web_url"`
	ImageURL string `json:"image_url"`
}

type Section struct {
	SectionName string   `json:"section_name"`
	Widgets     []Widget `json:"widgets"`
}

type Widget struct {
	WidgetType          string     `json:"widget_type"`
	Data                WidgetData `json:"data"`
	VisibilityCondition []string   `json:"visibility_condition"`
	UID                 string     `json:"uid"`
	ActionLog           *ActionLog `json:"action_log,omitempty"`
}

type ActionLog struct {
	ServerSideInfo ServerSideInfo `json:"server_side_info"`
	Enabled        bool           `json:"enabled"`
}

type ServerSideInfo struct {
	Info     AdditionalData `json:"info"`
	ItemType ItemType       `json:"item_type"`
}

type ItemType struct {
	Type string `json:"type"`
}

type WidgetData struct {
	Type                 string            `json:"@type"`
	ParentItems          []ParentItem      `json:"parent_items,omitempty"`
	CurrentPageTitle     string            `json:"current_page_title,omitempty"`
	Padded               bool              `json:"padded,omitempty"`
	Title                string            `json:"title,omitempty"`
	Subtitle             string            `json:"subtitle,omitempty"`
	HasDivider           bool              `json:"has_divider,omitempty"`
	ImageURL             string            `json:"image_url,omitempty"`
	Tags                 []string          `json:"tags,omitempty"`
	ShowThumbnail        bool              `json:"show_thumbnail,omitempty"`
	MobileDesign         bool              `json:"mobile_design,omitempty"`
	HighLevelHeading     bool              `json:"high_level_heading,omitempty"`
	Action               *Action2          `json:"action,omitempty"`
	HasNotification      bool              `json:"has_notification,omitempty"`
	Icon                 *Icon             `json:"icon,omitempty"`
	NotificationText     string            `json:"notification_text,omitempty"`
	Description          string            `json:"description,omitempty"`
	HasArrow             bool              `json:"has_arrow,omitempty"`
	Small                bool              `json:"small,omitempty"`
	LastNotificationDate string            `json:"last_notification_date,omitempty"`
	UID                  string            `json:"uid,omitempty"`
	Fullwidth            bool              `json:"fullwidth,omitempty"`
	Text                 string            `json:"text,omitempty"`
	Color                string            `json:"color,omitempty"`
	TextColor            string            `json:"text_color,omitempty"`
	DescriptionColor     string            `json:"description_color,omitempty"`
	SubtitleColor        string            `json:"subtitle_color,omitempty"`
	TextType             string            `json:"text_type,omitempty"`
	ShowRequired         bool              `json:"show_required,omitempty"`
	State                string            `json:"state,omitempty"`
	IsStickyHeader       bool              `json:"is_sticky_header,omitempty"`
	IsPrimary            bool              `json:"is_primary,omitempty"`
	Expandable           bool              `json:"expandable,omitempty"`
	PreviewMaxLine       int               `json:"preview_max_line,omitempty"`
	Items                []Item            `json:"items,omitempty"`
	ImageAspectRatio     *ImageAspectRatio `json:"image_aspect_ratio,omitempty"`
	HasPreview           bool              `json:"has_preview,omitempty"`
	HasThumbnails        bool              `json:"has_thumbnails,omitempty"`
	ScaleType            string            `json:"scale_type,omitempty"`
	ShowTooltip          bool              `json:"show_tooltip,omitempty"`
	TooltipData          *TooltipData      `json:"tooltip_data,omitempty"`
	Value                string            `json:"value,omitempty"`
	Compact              bool              `json:"compact,omitempty"`
	HasCopyToClipboard   bool              `json:"has_copy_to_clipboard,omitempty"`
	SendActionLogOnCopy  bool              `json:"send_action_log_on_copy,omitempty"`
	ImageColor           string            `json:"image_color,omitempty"`
	Disabled             bool              `json:"disabled,omitempty"`
	Credit               *Credit           `json:"credit,omitempty"`
	Rent                 *Credit           `json:"rent,omitempty"`
	Label                string            `json:"label,omitempty"`
	ActionTitle          string            `json:"action_title,omitempty"`
	TitleColor           string            `json:"title_color,omitempty"`
	Padding              string            `json:"padding,omitempty"`
	ActionText           string            `json:"action_text,omitempty"`
	ChipList             *ChipList         `json:"chip_list,omitempty"`
	Location             *Location         `json:"location,omitempty"`
	PreviewType          string            `json:"preview_type,omitempty"`
	ButtonTitle          string            `json:"button_title,omitempty"`
	PostToken            string            `json:"post_token,omitempty"`
}

type Location struct {
	Type      string     `json:"type"`
	ExactData *ExactData `json:"exact_data"`
	FuzzyData *FuzzyData `json:"fuzzy_data"`
}

// FuzzyData is an approximate location: the listing is within Radius meters of Point.
type FuzzyData struct {
	Point  Point   `json:"point"`
	Radius float64 `json:"radius"`
}

type ExactData struct {
	Point Point `json:"point"`
}

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type ChipList struct {
	Chips []Chip `json:"chips"`
}

type Chip struct {
	Text    string  `json:"text"`
	Type    string  `json:"type"`
	Style   string  `json:"style"`
	Rounded bool    `json:"rounded"`
	Small   bool    `json:"small"`
	Action  *Action `json:"action"`
}

type Credit struct {
	Value            string `json:"value"`
	TransformedValue string `json:"transformed_value"`
}

type TooltipData struct {
	Icon *Icon  `json:"icon"`
	Text string `json:"text"`
}

type ImageAspectRatio struct {
	DynamicAspectRatio DynamicAspectRatio `json:"dynamic_aspect_ratio"`
}

type DynamicAspectRatio struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type Item struct {
	Title     string `json:"title"`
	Value     string `json:"value,omitempty"`
	Icon      *Icon  `json:"icon,omitempty"`
	Available bool   `json:"available,omitempty"`
}

type Icon struct {
	ImageURLDark  string `json:"image_url_dark"`
	ImageURLLight string `json:"image_url_light"`
	IconName      string `json:"icon_name"`
	IconColor     string `json:"icon_color"`
}

type Action2 struct {
	Type        string   `json:"type"`
	Payload     Payload2 `json:"payload"`
	PagePopLink bool     `json:"page_pop_link"`
}

type Payload2 struct {
	Type            string         `json:"@type"`
	AdditionalData  AdditionalData `json:"additional_data"`
	Page            string         `json:"page"`
	IsModal         bool           `json:"is_modal"`
	RestRequestPath string         `json:"rest_request_path"`
	GrpcRequestPath string         `json:"grpc_request_path"`
}

type AdditionalData struct {
	Type      string `json:"@type"`
	PostToken string `json:"post_token"`
}

type ParentItem struct {
	Title  string  `json:"title"`
	Action *Action `json:"action"`
}

type Action struct {
	Type        string  `json:"type"`
	Payload     Payload `json:"payload"`
	PagePopLink bool    `json:"page_pop_link"`
}

type Payload struct {
	Type          string     `json:"@type"`
	SearchData    SearchData `json:"search_data"`
	SourceView    string     `json:"source_view"`
	Tooltip       string     `json:"tooltip"`
	Refresh       bool       `json:"refresh"`
	SourceViewStr string     `json:"source_view_str"`
	IsNotLink     bool       `json:"is_not_link"`
	Referer       string     `json:"referer"`
}

type SearchData struct {
	FormData                  FormData `json:"form_data"`
	Query                     string   `json:"query"`
	QueryInputType            string   `json:"query_input_type"`
	OnlineRequestResponseData struct{} `json:"online_request_response_data"`
}

type FormData struct {
	Data FormDataContent `json:"data"`
}

type FormDataContent struct {
	Category CategoryData `json:"category"`
}

type CategoryData struct {
	Str StringValue `json:"str"`
}

type StringValue struct {
	Value string `json:"value"`
}

var APIPaths = APIPath{
	SearchList: "/v8/postlist/w/search",
	PostDetail: "/v8/posts-v2/web",
}

func GetPostContext(ctx context.Context, postToken string) (PostDetailRes, error) {
	postToken = strings.TrimSpace(postToken)
	if postToken == "" {
		return PostDetailRes{}, errors.New("post token is required")
	}

	// Set default headers
	headers := map[string]string{
		"accept":          "application/json-filled",
		"accept-language": "en-US,en;q=0.9",
	}

	// Construct URL
	url := fmt.Sprintf("https://api.divar.ir%s/%s", APIPaths.PostDetail, postToken)

	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return PostDetailRes{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read response
	output, err := io.ReadAll(resp.Body)
	if err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to read response: %w", err)
	}

	var data PostDetailRes
	if err := json.Unmarshal(output, &data); err != nil {
		return PostDetailRes{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return data, nil
}

// PostImportantData contains general listing data plus optional rental-specific context.
type PostImportantData struct {
	Token           string                `json:"token"`
	Title           string                `json:"title"`
	Subtitle        string                `json:"subtitle"`
	Description     string                `json:"description"`
	Price           RentalPriceInfo       `json:"price"`
	PropertyDetails RentalPropertyDetails `json:"details"`
	Location        LocationInfo          `json:"location"`
	Images          []string              `json:"images"`
	Features        []FeatureInfo         `json:"features"`
	Attributes      map[string]string     `json:"attributes"`
	ContactInfo     ContactDetails        `json:"contact"`
	City            string                `json:"city"`
	CityName        string                `json:"city_name"`
	Category        string                `json:"category"`
	PostedDate      string                `json:"posted_date"`
	ExpiresAt       string                `json:"expires_at"`
	URL             string                `json:"url"`
}

// RentalPriceInfo contains pricing information for rental listings (deposit + monthly rent)
type RentalPriceInfo struct {
	Deposit      string `json:"deposit"`
	Rent         string `json:"rent"`
	TransDeposit string `json:"trans_deposit"`
	TransRent    string `json:"trans_rent"`
	// RealMonthlyRent is the real monthly cost in today's toman, combining
	// deposit and rent under inflation; zero when not a rental.
	RealMonthlyRent int64 `json:"real_monthly_rent,omitempty"`
}

// RentalPropertyDetails contains property-specific details for rental listings
type RentalPropertyDetails struct {
	Area      string `json:"area"`
	BuildYear string `json:"build_year"`
	Rooms     string `json:"rooms"`
	Floor     string `json:"floor"`
}

type LocationInfo struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
	// RadiusMeters is set for approximate locations; zero means exact.
	RadiusMeters float64 `json:"radius_meters,omitempty"`
	MapImage     string  `json:"map_image"`
}

// HasPoint reports whether the listing published coordinates.
func (l LocationInfo) HasPoint() bool {
	return l.Latitude != 0 || l.Longitude != 0
}

type FeatureInfo struct {
	Title     string `json:"title"`
	Available bool   `json:"available"`
}

type ContactDetails struct {
	ChatEnabled       bool   `json:"chat_enabled"`
	ContactUUID       string `json:"contact_uuid"`
	SecureCallEnabled bool   `json:"secure_call_enabled"`
}

// ExtractImportantData extracts general listing data and preserves useful rental fields when present.
func ExtractImportantData(post PostDetailRes) PostImportantData {
	data := PostImportantData{
		Token:      post.Webengage.Token,
		City:       post.Webengage.City,
		CityName:   post.City.Name,
		Category:   post.Webengage.Category,
		URL:        post.Share.WebURL,
		ExpiresAt:  post.Seo.UnavailableAfter,
		Attributes: map[string]string{},
		ContactInfo: ContactDetails{
			ChatEnabled:       post.Contact.ChatEnabled,
			ContactUUID:       post.Contact.ContactUUID,
			SecureCallEnabled: post.Contact.SecureCallEnabled,
		},
	}
	if post.Webengage.BrandModel != "" {
		data.Attributes["brand_model"] = post.Webengage.BrandModel
	}
	if post.Webengage.Originality != "" {
		data.Attributes["originality"] = post.Webengage.Originality
	}
	if post.Webengage.BusinessType != "" {
		data.Attributes["business_type"] = post.Webengage.BusinessType
	}
	if post.Webengage.Status != "" {
		data.Attributes["status"] = post.Webengage.Status
	}
	if post.Webengage.Price > 0 {
		data.Attributes["price"] = strconv.Itoa(post.Webengage.Price)
	}

	// Extract data from sections
	for _, section := range post.Sections {
		switch section.SectionName {
		case "BREADCRUMB", "POST_TITLE":
			// Extract title and subtitle from first widget
			if len(section.Widgets) > 0 {
				widget := section.Widgets[0]
				if data.Title == "" && widget.Data.Title != "" {
					data.Title = widget.Data.Title
				}
				if data.Subtitle == "" && widget.Data.Subtitle != "" {
					data.Subtitle = widget.Data.Subtitle
				}
			}

		case "DESCRIPTION":
			// Extract description
			for _, widget := range section.Widgets {
				if widget.WidgetType == "DESCRIPTION_ROW" && widget.Data.Text != "" {
					data.Description = widget.Data.Text
					break
				}
			}

		case "IMAGE":
			// Extract images
			for _, widget := range section.Widgets {
				if widget.WidgetType == "IMAGE_CAROUSEL" && len(widget.Data.Items) > 0 {
					for _, item := range widget.Data.Items {
						if item.Icon != nil && item.Icon.ImageURLLight != "" {
							data.Images = append(data.Images, item.Icon.ImageURLLight)
						}
					}
					break
				}
			}

		case "LIST_DATA":
			// Extract property details, price, floor, and features
			for i, widget := range section.Widgets {
				// Rent and deposit come in UNEXPANDABLE_ROW or RENT_SLIDER
				// widgets; a slider's transformed values are the converted terms.
				if widget.Data.Credit != nil {
					data.Price.Deposit = widget.Data.Credit.Value
					data.Price.TransDeposit = widget.Data.Credit.TransformedValue
				}
				if widget.Data.Rent != nil {
					data.Price.Rent = widget.Data.Rent.Value
					data.Price.TransRent = widget.Data.Rent.TransformedValue
				}

				switch widget.WidgetType {
				case "GROUP_INFO_ROW":
					// Property details (area, build year, rooms)
					if len(widget.Data.Items) >= 3 {
						if widget.Data.Items[0].Title == "متراژ" {
							data.PropertyDetails.Area = widget.Data.Items[0].Value
						}
						if widget.Data.Items[1].Title == "ساخت" {
							data.PropertyDetails.BuildYear = widget.Data.Items[1].Value
						}
						if widget.Data.Items[2].Title == "اتاق" {
							data.PropertyDetails.Rooms = widget.Data.Items[2].Value
						}
					}

				case "UNEXPANDABLE_ROW":
					// Check for floor information
					if widget.Data.Title == "طبقه" && widget.Data.Value != "" {
						data.PropertyDetails.Floor = widget.Data.Value
					}

				case "GROUP_FEATURE_ROW":
					// Extract features
					if len(widget.Data.Items) > 0 {
						for _, item := range widget.Data.Items {
							data.Features = append(data.Features, FeatureInfo{
								Title:     item.Title,
								Available: item.Available,
							})
						}
					}
				}

				// Extract posted date from subtitle if available in early widgets
				if i == 0 && data.PostedDate == "" && widget.Data.Subtitle != "" {
					data.PostedDate = widget.Data.Subtitle
				}
			}

		case "MAP":
			// Extract location
			for _, widget := range section.Widgets {
				if widget.WidgetType == "MAP_ROW" && widget.Data.Location != nil {
					if exact := widget.Data.Location.ExactData; exact != nil {
						data.Location.Latitude = exact.Point.Latitude
						data.Location.Longitude = exact.Point.Longitude
					} else if fuzzy := widget.Data.Location.FuzzyData; fuzzy != nil {
						data.Location.Latitude = fuzzy.Point.Latitude
						data.Location.Longitude = fuzzy.Point.Longitude
						data.Location.RadiusMeters = fuzzy.Radius
					}
					if widget.Data.ImageURL != "" {
						data.Location.MapImage = widget.Data.ImageURL
					}
					break
				}
			}
		}
	}

	// Preserve category-specific key/value data such as mileage, color, or device condition.
	for _, section := range post.Sections {
		for _, widget := range section.Widgets {
			if widget.Data.Title != "" && widget.Data.Value != "" {
				data.Attributes[widget.Data.Title] = widget.Data.Value
			}
			for _, item := range widget.Data.Items {
				if item.Title != "" && item.Value != "" {
					data.Attributes[item.Title] = item.Value
				}
			}
		}
	}

	// Fixed (non-convertible) rentals show their terms as plain rows.
	for title, value := range data.Attributes {
		switch NormalizePersian(title) {
		case "ودیعه":
			if data.Price.Deposit == "" {
				data.Price.Deposit = value
			}
		case "اجاره ماهانه":
			if data.Price.Rent == "" {
				data.Price.Rent = value
			}
		}
	}

	// Fallback: If price data was not found in widgets, try to extract from
	// Webengage, whose amounts can be slightly off (float precision).
	if data.Price.Deposit == "" && post.Webengage.Credit > 0 {
		data.Price.Deposit = fmt.Sprintf("%d", post.Webengage.Credit)
	}
	if data.Price.Rent == "" && post.Webengage.Rent > 0 {
		data.Price.Rent = fmt.Sprintf("%d", post.Webengage.Rent)
	}

	return data
}
