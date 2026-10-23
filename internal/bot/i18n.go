package bot

import "fmt"

// Strings holds all localised UI text for one language.
type Strings struct {
	// Onboarding
	PinPrompt     string
	InvalidPin    string
	LeaderPrompt  string // %s = pincode
	SelectLeader  string // list button label
	LeaderSection string // list section title

	// Main menu
	MenuHeading     string // %[1]s = leader name
	MenuInstruction string
	MenuButton      string // list button label
	SectionOne      string
	SectionTwo      string
	OptMoreOptions  string // "More options" row revealing the rest of the menu

	// Option labels (menu rows)
	OptRaiseComplaint  string
	OptTrackComplaint  string
	OptScheduleMeeting string
	OptAboutLeader     string
	OptTransparency    string
	OptWorkReports     string
	OptEvents          string
	OptSchemes         string
	OptJanSeva         string
	OptNariShakti      string
	OptSwadeshi        string
	OptYouth           string
	OptJoinTeam        string

	// Complaint sub-flow
	ComplaintPrompt  string
	InvalidDetails   string
	ComplaintSuccess string // %s = ticket ID

	// Track sub-flow
	TrackPrompt     string
	TrackInProgress string // %s = ticket ID
	TrackResolved   string // %s = ticket ID
	TrackInvalid    string

	// Meeting sub-flow
	MeetingPrompt    string
	MeetingSlots     string
	MeetingSuccess   string
	MeetingNoSlots   string
	MeetingSlotTaken string
	MeetingFailed    string
	SlotsButton      string // list button label
	SlotsSection     string // list section title

	// API failures (complaint / track flows)
	ComplaintFailed string
	TrackFailed     string

	// Info replies (options 4–13)
	AboutLeaderText  string // %[1]s = leader name
	TransparencyText string
	WorkReportsText  string
	EventsText       string
	SchemesText      string
	JanSevaText      string
	NariShaktiText   string
	SwadeshiText     string
	YouthText        string
	JoinTeamText     string
	ThankYou         string
	InfoMoreLink     string // footer appended to API-backed listings

	// Headers for API-backed listings (options 4–9, 12)
	TransparencyHeader string
	WorkReportsHeader  string
	EventsHeader       string
	SchemesHeader      string
	JanSevaHeader      string
	YouthHeader        string

	// Generic labels
	LabelEnglish string
	LabelMarathi string
	LabelHindi   string
}

// GreetingFor returns a personalised greeting with the user's real phone number.
// phone is the E.164 number from the webhook (e.g. "919812345678").
// The greeting is intentionally trilingual (EN / MR / HI) before any language
// has been chosen.
func GreetingFor(lang, phone string) string {
	formatted := formatPhone(phone)

	return fmt.Sprintf(`👋 Hi *%s*
Please select your preferred language from below 👇!!

कृपया तुमची पसंतीची भाषा निवडा 👇!!

कृपया नीचे से अपनी पसंदीदा भाषा चुनें 👇!!`, formatted)
}

// formatPhone converts "919812345678" → "+91 98123 45678"
func formatPhone(raw string) string {
	digits := raw
	if len(digits) > 0 && digits[0] == '+' {
		digits = digits[1:]
	}
	// Indian numbers arrive as 91XXXXXXXXXX (12 digits)
	if len(digits) == 12 && digits[:2] == "91" {
		local := digits[2:] // 10 digits
		return fmt.Sprintf("+91 %s %s", local[:5], local[5:])
	}
	// Fallback: just prefix "+"
	return "+" + digits
}

// partyName returns the BJP party label for a language.
func partyName(lang string) string {
	switch lang {
	case "hi":
		return "भारतीय जनता पार्टी (भाजपा)"
	case "mr":
		return "भारतीय जनता पक्ष (भाजप)"
	default:
		return "BJP"
	}
}

// I18n maps a language code to its Strings.
// NOTE: Greeting is intentionally left out here — use GreetingFor() instead
// so the user's real number is injected at runtime.
var I18n = map[string]Strings{
	"en": {
		PinPrompt: `This is the *Ataljanseva Citizen Service* Automatic Reply Chatbot.

To continue further, please enter your *6-digit PIN Code* in the following format 👇!!.

📍 *PIN Code*
Example: *400601*`,

		InvalidPin: `The entered PIN Code *%s* could not be found or do not match our records.

Please re-enter the correct *6-digit PIN Code* in the following format to continue 👇

📍 *PIN Code*
Example: *400601*`,

		LeaderPrompt: `You entered PIN Code *%s* successfully.

Please select your Leader from the list below 👇!!`,

		SelectLeader:  "Select Leader",
		LeaderSection: "🏅 Leaders",

		MenuHeading:     "🎖️ *%[1]s*\n🎖️ BJP\n\nWelcome to %[1]s's E-Office",
		MenuInstruction: "Select Option 👇",
		MenuButton:      "Select Option",
		SectionOne:      "Services",
		SectionTwo:      "More Services",
		OptMoreOptions:  "More Options (10-13)",

		OptRaiseComplaint:  "Raise Complaint",
		OptTrackComplaint:  "Track Complaint",
		OptScheduleMeeting: "Schedule Meeting",
		OptAboutLeader:     "About Leader",
		OptTransparency:    "Transparency Board",
		OptWorkReports:     "Work Reports",
		OptEvents:          "Events & Meetings",
		OptSchemes:         "Schemes & Services",
		OptJanSeva:         "JanSeva Connect",
		OptNariShakti:      "NariShakti",
		OptSwadeshi:        "Swadeshi - Made in India",
		OptYouth:           "Youth Engagement Campaign",
		OptJoinTeam:        "Join Our Team",

		ComplaintPrompt: `To raise your complaint, please send your details in this format:

Enter your full name:
Describe your complaint:
Enter the address or share the location where this complaint occurred:

Example:
Enter your full name: Amit Sharma
Describe your complaint: Garbage not collected in our area since 3 days
Enter the address or share the location where this complaint occurred: Near Hanuman Mandir, Jintur`,

		InvalidDetails: `We couldn't understand the details you sent.

Please try again in the format below 👇`,

		ComplaintSuccess: `Your complaint has been raised successfully!
Your Ticket ID is: *%s*
You can use this ID to track your complaint status. Thank you!`,

		ComplaintFailed: `Sorry, we couldn't submit your complaint right now.
Please try again in a moment.`,

		TrackPrompt: `To track your complaint, please enter your Ticket ID:`,
		TrackInProgress: `Ticket ID: *%s*

Your complaint is in progress.`,
		TrackResolved: `Ticket ID: *%s*

Your complaint has been resolved.`,
		TrackInvalid: `Invalid Ticket ID.
Please check and enter the correct Ticket ID.`,
		TrackFailed: `We couldn't fetch your complaint status right now.
Please try again later.`,

		MeetingPrompt: `To schedule a meeting, please share your details in this format:

Full Name:
Preferred Date (DD-MM-YYYY):
Reason for Appointment:`,

		MeetingSlots:   `Please choose from the available slots:`,
		MeetingSuccess: `Your appointment has been scheduled successfully.`,
		MeetingNoSlots: `All slots for this date are already booked. Please choose another date.`,
		MeetingSlotTaken: `That time slot was just booked by someone else.
Please choose another available time.`,
		MeetingFailed: `Sorry, we couldn't book your appointment right now.
Please try again in a moment.`,
		SlotsButton:  "Select Slot",
		SlotsSection: "Available Slots",

		AboutLeaderText: `To know more details about %s, please visit: ataljanseva.in`,

		TransparencyText: `To view the Transparency Board, Work Reports, Event & Meeting details, please visit: ataljanseva.in`,

		WorkReportsText: `To view the Transparency Board, Work Reports, Event & Meeting details, please visit: ataljanseva.in`,

		EventsText: `To view the Transparency Board, Work Reports, Event & Meeting details, please visit: ataljanseva.in`,

		SchemesText: `To avail the benefits of various schemes and services, please visit: ataljanseva.in`,

		JanSevaText: `JanSeva Connect focuses on Vocal for Local. With this feature, citizens can offer or explore local services, find job opportunities, and grow their business.
To know more about JanSeva Connect, please visit: ataljanseva.in`,

		NariShaktiText: `NariShakti focuses on women's financial independence.
With this feature, women can offer or explore local services, find job opportunities, and grow their business. To know more about NariShakti, visit: ataljanseva.in`,

		SwadeshiText: `Support Swadeshi, Support India!
Buy and Sell Made-in-India products on ataljanseva.in, grow your business and make our nation Atmanirbhar!`,

		YouthText: `Youth Engagement Campaigns
A social initiative where leaders and citizens come together to work for the betterment of society, creating a positive impact that benefits everyone.
To know more about Social Campaigns, visit: ataljanseva.in`,

		JoinTeamText: `To become a part of our team, please visit ataljanseva.in and apply now.`,

		ThankYou: `Thank you for staying connected with the *Ataljanseva Citizen Service*!!`,

		InfoMoreLink: `For more details, please visit: ataljanseva.in`,

		TransparencyHeader: `Recent resolved grievances:`,
		WorkReportsHeader:  `Latest work reports:`,
		EventsHeader:       `Upcoming events & meetings:`,
		SchemesHeader:      `Government schemes & services:`,
		JanSevaHeader:      `Local services near you:`,
		YouthHeader:        `Latest social campaigns:`,

		LabelEnglish: "English",
		LabelMarathi: "मराठी",
		LabelHindi:   "हिंदी",
	},

	"mr": {
		PinPrompt: `हे अटलजनसेवा नागरिक सेवेची ऑटोमॅटिक रिप्लाय चॅटबॉट आहे.

पुढे जाण्यासाठी कृपया आपला ६ अंकी पिन कोड खालील स्वरूपात प्रविष्ट करा 👇!!

📍 *पिन कोड*
उदाहरण: *४००६०१*`,

		InvalidPin: `प्रविष्ट केलेला पिन कोड *%s* आमच्या नोंदींशी जुळत नाही किंवा सापडला नाही.

पुढे जाण्यासाठी कृपया आपला योग्य *६ अंकी पिन कोड* खालील स्वरूपात पुन्हा प्रविष्ट करा 👇!!

📍 *पिन कोड*
उदाहरण: *४००६०१*`,

		LeaderPrompt: `आपण पिन कोड *%s* यशस्वीरित्या टाकला आहे.

कृपया खालील यादीमधून आपल्या नेत्याची निवड करा 👇!!`,

		SelectLeader:  "नेता निवडा",
		LeaderSection: "नेते",

		MenuHeading:     "🎖️ *%[1]s*\n🎖️ भाजपा\n\n%[1]s ई-ऑफिसमध्ये आपले स्वागत आहे",
		MenuInstruction: "पर्याय निवडा 👇",
		MenuButton:      "पर्याय निवडा",
		SectionOne:      "सेवा",
		SectionTwo:      "अधिक सेवा",
		OptMoreOptions:  "अधिक पर्याय (10-13)",

		OptRaiseComplaint:  "तक्रार नोंदवा",
		OptTrackComplaint:  "तक्रारीची स्थिती तपासा",
		OptScheduleMeeting: "भेटीची वेळ निश्चित करा",
		OptAboutLeader:     "नेत्याबद्दल माहिती",
		OptTransparency:    "पारदर्शकता बोर्ड",
		OptWorkReports:     "कामाचा अहवाल",
		OptEvents:          "कार्यक्रम आणि बैठका",
		OptSchemes:         "योजना आणि सेवा",
		OptJanSeva:         "जनसेवा कनेक्ट",
		OptNariShakti:      "नारीशक्ती",
		OptSwadeshi:        "स्वदेशी - मेड इन इंडिया",
		OptYouth:           "युवा सहभाग मोहीम",
		OptJoinTeam:        "आमच्या टीममध्ये सामील व्हा",

		ComplaintPrompt: `आपली तक्रार नोंदवण्यासाठी, कृपया आपली माहिती या फॉरमॅटमध्ये पाठवा:
आपले पूर्ण नाव टाका:
आपल्या तक्रारीचे वर्णन करा:
जिथे ही तक्रार घडली आहे त्या ठिकाणचा पत्ता टाका किंवा लोकेशन शेयर करा:

उदाहरण:
आपले पूर्ण नाव टाका: अमित शर्मा
आपल्या तक्रारीचे वर्णन करा: आमच्या भागात गेले 3 दिवस कचरा उचलला गेला नाही
जिथे ही तक्रार घडली आहे त्या ठिकाणचा पत्ता टाका किंवा लोकेशन शेयर करा: हनुमान मंदिराजवळ, जिंतूर`,

		InvalidDetails: `आपण पाठवलेली माहिती समजण्यात आली नाही.

कृपया खालील स्वरूपात पुन्हा प्रयत्न करा 👇`,

		ComplaintSuccess: `आपली तक्रार यशस्वीरित्या नोंदवली गेली आहे!
आपला तिकीट ID आहे: *%s*
आपण या ID द्वारे आपल्या तक्रारीची स्थिती तपासू शकता। धन्यवाद!`,

		ComplaintFailed: `क्षमस्व, आपली तक्रार आत्ता सादर करता आली नाही.
कृपया थोड्या वेळाने पुन्हा प्रयत्न करा.`,

		TrackPrompt: `आपल्या तक्रारीचा मागोवा घेण्यासाठी, कृपया आपला तिकीट ID टाका:`,
		TrackInProgress: `तिकीट ID: *%s*

आपल्या तक्रारीवर काम सुरू आहे.`,
		TrackResolved: `तिकीट ID: *%s*

आपल्या तक्रारीचे निराकरण झाले आहे.`,
		TrackInvalid: `अवैध तिकीट ID
कृपया योग्य तिकीट ID तपासून पुन्हा टाका`,
		TrackFailed: `आपल्या तक्रारीची स्थिती आत्ता मिळवता आली नाही.
कृपया नंतर पुन्हा प्रयत्न करा.`,

		MeetingPrompt: `भेट निश्चित करण्यासाठी, कृपया आपली माहिती या स्वरूपात शेअर करा:
पूर्ण नाव:
पसंतीची तारीख (DD-MM-YYYY):
भेटीचे कारण:`,

		MeetingSlots:   `कृपया उपलब्ध वेळांमधून निवडा:`,
		MeetingSuccess: `आपली भेट यशस्वीरित्या निश्चित करण्यात आली आहे.`,
		MeetingNoSlots: `या तारखेच्या सर्व वेळा भरून गेल्या आहेत. कृपया दुसरी तारीख निवडा.`,
		MeetingSlotTaken: `ही वेळ आधीच भरली आहे.
कृपया उपलब्ध दुसरी वेळ निवडा.`,
		MeetingFailed: `क्षमस्व, आपली भेट आत्ता निश्चित करता आली नाही.
कृपया थोड्या वेळाने पुन्हा प्रयत्न करा.`,
		SlotsButton:  "वेळ निवडा",
		SlotsSection: "उपलब्ध वेळा",

		AboutLeaderText: `%s यांच्याबद्दल अधिक माहितीसाठी, कृपया भेट द्या: ataljanseva.in`,

		TransparencyText: `पारदर्शकता फलक, कामाचा अहवाल, कार्यक्रम आणि बैठकीची माहिती पाहण्यासाठी, कृपया भेट द्या: ataljanseva.in`,

		WorkReportsText: `पारदर्शकता फलक, कामाचा अहवाल, कार्यक्रम आणि बैठकीची माहिती पाहण्यासाठी, कृपया भेट द्या: ataljanseva.in`,

		EventsText: `पारदर्शकता फलक, कामाचा अहवाल, कार्यक्रम आणि बैठकीची माहिती पाहण्यासाठी, कृपया भेट द्या: ataljanseva.in`,

		SchemesText: `विविध योजना आणि सेवांचा लाभ घेण्यासाठी, कृपया भेट द्या: ataljanseva.in`,

		JanSevaText: `जनसेवा कनेक्ट 'व्होकल फॉर लोकल' वर लक्ष केंद्रित करते. या सुविधेद्वारे नागरिक स्थानिक सेवा देऊ किंवा घेऊ शकतात, नोकरीच्या संधी शोधू शकतात आणि आपला व्यवसाय वाढवू शकतात.
जनसेवा कनेक्ट बद्दल अधिक जाणून घेण्यासाठी, कृपया भेट द्या: ataljanseva.in`,

		NariShaktiText: `नारीशक्ती महिलांच्या आर्थिक स्वावलंबनावर लक्ष केंद्रित करते.
या सुविधेद्वारे महिला स्थानिक सेवा देऊ किंवा घेऊ शकतात, नोकरीच्या संधी शोधू शकतात आणि आपला व्यवसाय वाढवू शकतात. नारीशक्ती बद्दल अधिक जाणून घेण्यासाठी भेट द्या: ataljanseva.in`,

		SwadeshiText: `स्वदेशीला पाठिंबा द्या, भारताला पाठिंबा द्या!
ataljanseva.in वर Made-in-India उत्पादने खरेदी आणि विक्री करा, आपला व्यवसाय वाढवा आणि आपला देश आत्मनिर्भर बनवा!`,

		YouthText: `युवा सहभाग मोहीम
एक सामाजिक उपक्रम जिथे नेते आणि नागरिक समाजाच्या हितासाठी एकत्र येऊन काम करतात, सकारात्मक बदल घडवतात ज्याचा फायदा सर्वांना होतो.
सामाजिक मोहिमांबद्दल अधिक जाणून घेण्यासाठी भेट द्या: ataljanseva.in`,

		JoinTeamText: `आमच्या टीमचा भाग होण्यासाठी, कृपया ataljanseva.in ला भेट द्या आणि आत्ताच अर्ज करा.`,

		ThankYou: `*अटलजनसेवा नागरिक सेवेशी* जोडलेले राहिल्याबद्दल धन्यवाद !!`,

		InfoMoreLink: `अधिक माहितीसाठी, कृपया भेट द्या: ataljanseva.in`,

		TransparencyHeader: `निराकरण झालेल्या तक्रारी:`,
		WorkReportsHeader:  `नवीनतम कामाचे अहवाल:`,
		EventsHeader:       `आगामी कार्यक्रम आणि बैठका:`,
		SchemesHeader:      `सरकारी योजना आणि सेवा:`,
		JanSevaHeader:      `जवळच्या स्थानिक सेवा:`,
		YouthHeader:        `नवीनतम सामाजिक मोहिमा:`,

		LabelEnglish: "English",
		LabelMarathi: "मराठी",
		LabelHindi:   "हिंदी",
	},

	"hi": {
		PinPrompt: `*अटलजनसेवा नागरिक सेवा* ऑटोमेटिक रिप्लाई चैटबॉट है।

आगे बढ़ने के लिए कृपया अपना ६ अंकों का पिन कोड नीचे दिए गए प्रारूप में दर्ज करें 👇!!

📍 *पिन कोड*
उदाहरण: *४००६०१*`,

		InvalidPin: `आपके द्वारा दर्ज की गई पिन कोड *%s* गलत है या उपलब्ध नहीं है।

आगे बढ़ने के लिए कृपया अपना सही ६ अंकों का पिन कोड नीचे दिए गए प्रारूप में पुनः दर्ज करें 👇

📍 *पिन कोड*
उदाहरण: *४००६०१*`,

		LeaderPrompt: `आपने पिन कोड *%s* सफलतापूर्वक दर्ज किया है।

कृपया नीचे दी गई सूची में से अपने नेता का चयन करें 👇!!`,

		SelectLeader:  "नेता चुनें",
		LeaderSection: "नेता",

		MenuHeading:     "🎖️ *%[1]s*\n🎖️ भाजपा\n\n%[1]s के ई-ऑफिस में आपका स्वागत है",
		MenuInstruction: "विकल्प चुनें 👇",
		MenuButton:      "विकल्प चुनें",
		SectionOne:      "सेवाएं",
		SectionTwo:      "अधिक सेवाएं",
		OptMoreOptions:  "अधिक विकल्प (10-13)",

		OptRaiseComplaint:  "शिकायत दर्ज करें",
		OptTrackComplaint:  "शिकायत की स्थिति देखें",
		OptScheduleMeeting: "मीटिंग तय करें",
		OptAboutLeader:     "नेता के बारे में",
		OptTransparency:    "पारदर्शिता बोर्ड",
		OptWorkReports:     "कार्य रिपोर्ट",
		OptEvents:          "कार्यक्रम और बैठकें",
		OptSchemes:         "योजनाएं और सेवाएं",
		OptJanSeva:         "जनसेवा कनेक्ट",
		OptNariShakti:      "नारीशक्ति",
		OptSwadeshi:        "स्वदेशी - मेड इन इंडिया",
		OptYouth:           "युवा सहभाग अभियान",
		OptJoinTeam:        "हमारी टीम से जुड़ें",

		ComplaintPrompt: `अपनी शिकायत दर्ज करने के लिए, कृपया अपना विवरण इस फॉर्मेट में भेजें:
अपना पूरा नाम दर्ज करें:
अपनी शिकायत का वर्णन करें:
वह पता दर्ज करें या लोकेशन शेयर करें जहाँ यह शिकायत हुई है:

उदाहरण:
अपना पूरा नाम दर्ज करें: अमित शर्मा
अपनी शिकायत का वर्णन करें: हमारे इलाके में पिछले 3 दिनों से कचरा नहीं उठाया गया है
वह पता दर्ज करें या लोकेशन शेयर करें जहाँ यह शिकायत हुई है: हनुमान मंदिर के पास, जिंतूर`,

		InvalidDetails: `हम आपके द्वारा भेजी गई जानकारी को समझ नहीं सके।

कृपया नीचे दिए गए प्रारूप में पुनः प्रयास करें 👇`,

		ComplaintSuccess: `आपकी शिकायत सफलतापूर्वक दर्ज कर ली गई है!
आपका टिकट ID है: *%s*
आप इस ID से अपनी शिकायत की स्थिति देख सकते हैं। धन्यवाद!`,

		ComplaintFailed: `क्षमा करें, आपकी शिकायत अभी दर्ज नहीं हो सकी।
कृपया थोड़ी देर बाद पुनः प्रयास करें।`,

		TrackPrompt: `अपनी शिकायत को ट्रैक करने के लिए, कृपया अपना टिकट ID दर्ज करें:`,
		TrackInProgress: `टिकट ID: *%s*

आपकी शिकायत पर काम चल रहा है।`,
		TrackResolved: `टिकट ID: *%s*

आपकी शिकायत का समाधान हो गया है।`,
		TrackInvalid: `अमान्य टिकट ID।
कृपया सही टिकट ID जाँच कर दर्ज करें।`,
		TrackFailed: `हम अभी आपकी शिकायत की स्थिति प्राप्त नहीं कर सके।
कृपया बाद में पुनः प्रयास करें।`,

		MeetingPrompt: `मीटिंग तय करने के लिए, कृपया अपनी जानकारी इस फॉर्मेट में साझा करें:
पूरा नाम:
पसंदीदा तारीख (DD-MM-YYYY):
अपॉइंटमेंट का कारण:`,

		MeetingSlots:   `कृपया उपलब्ध समय में से चुनें:`,
		MeetingSuccess: `आपकी अपॉइंटमेंट सफलतापूर्वक तय कर दी गई है।`,
		MeetingNoSlots: `इस तारीख के सभी समय भर चुके हैं। कृपया कोई दूसरी तारीख चुनें।`,
		MeetingSlotTaken: `यह समय पहले ही भर चुका है।
कृपया कोई उपलब्ध दूसरा समय चुनें।`,
		MeetingFailed: `क्षमा करें, आपकी अपॉइंटमेंट अभी तय नहीं हो सकी।
कृपया थोड़ी देर बाद पुनः प्रयास करें।`,
		SlotsButton:  "समय चुनें",
		SlotsSection: "उपलब्ध समय",

		AboutLeaderText: `%s के बारे में अधिक जानकारी के लिए, कृपया देखें: ataljanseva.in`,

		TransparencyText: `पारदर्शिता बोर्ड, कार्य रिपोर्ट, कार्यक्रम और बैठक की जानकारी देखने के लिए, कृपया देखें: ataljanseva.in`,

		WorkReportsText: `पारदर्शिता बोर्ड, कार्य रिपोर्ट, कार्यक्रम और बैठक की जानकारी देखने के लिए, कृपया देखें: ataljanseva.in`,

		EventsText: `पारदर्शिता बोर्ड, कार्य रिपोर्ट, कार्यक्रम और बैठक की जानकारी देखने के लिए, कृपया देखें: ataljanseva.in`,

		SchemesText: `विभिन्न योजनाओं और सेवाओं का लाभ उठाने के लिए, कृपया देखें: ataljanseva.in`,

		JanSevaText: `जनसेवा कनेक्ट 'वोकल फॉर लोकल' पर केंद्रित है। इस फीचर के माध्यम से नागरिक स्थानीय सेवाएं दे या ले सकते हैं, नौकरी के अवसर ढूंढ सकते हैं और अपने व्यवसाय को बढ़ा सकते हैं।
जनसेवा कनेक्ट के बारे में अधिक जानने के लिए, कृपया देखें: ataljanseva.in`,

		NariShaktiText: `नारीशक्ति महिला आर्थिक स्वतंत्रता पर केंद्रित है।
इस फीचर के माध्यम से महिलाएं स्थानीय सेवाएं दे या ले सकती हैं, नौकरी के अवसर ढूंढ सकती हैं और अपने व्यवसाय को बढ़ा सकती हैं। नारीशक्ति के बारे में अधिक जानने के लिए देखें: ataljanseva.in`,

		SwadeshiText: `स्वदेशी अपनाएं, भारत को सशक्त बनाएं!
ataljanseva.in पर Made-in-India उत्पाद खरीदें और बेचें, अपने व्यवसाय को बढ़ाएं और हमारे राष्ट्र को आत्मनिर्भर बनाएं!`,

		YouthText: `युवा सहभाग अभियान
एक सामाजिक पहल जहाँ नेता और नागरिक समाज की बेहतरी के लिए एक साथ आते हैं, एक सकारात्मक प्रभाव बनाते हैं जिससे सभी को लाभ होता है।
सामाजिक अभियानों के बारे में अधिक जानने के लिए देखें: ataljanseva.in`,

		JoinTeamText: `हमारी टीम का हिस्सा बनने के लिए, कृपया ataljanseva.in पर जाएं और अभी आवेदन करें।`,

		ThankYou: `अटलजनसेवा नागरिक सेवा से जुड़े रहने के लिए धन्यवाद !!`,

		InfoMoreLink: `अधिक जानकारी के लिए, कृपया देखें: ataljanseva.in`,

		TransparencyHeader: `हल की गई शिकायतें:`,
		WorkReportsHeader:  `नवीनतम कार्य रिपोर्ट:`,
		EventsHeader:       `आगामी कार्यक्रम और बैठकें:`,
		SchemesHeader:      `सरकारी योजनाएं और सेवाएं:`,
		JanSevaHeader:      `आपके पास की स्थानीय सेवाएं:`,
		YouthHeader:        `नवीनतम सामाजिक अभियान:`,

		LabelEnglish: "English",
		LabelMarathi: "मराठी",
		LabelHindi:   "हिंदी",
	},
}
