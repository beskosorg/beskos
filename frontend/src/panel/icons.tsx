// Monochrome icons (stroke, currentColor). They behave like an icon font: inherited color, size via prop.
type P = { size?: number };
const S = (size = 18) => ({
  width: size, height: size, viewBox: "0 0 24 24", fill: "none",
  stroke: "currentColor", strokeWidth: 1.9, strokeLinecap: "round" as const, strokeLinejoin: "round" as const,
});

// The star is the only icon with state: outlined = unstarred, filled = starred. `filled` paints
// the interior with the inherited color, so the color comes from CSS just like the others.
export const IcRefresh = ({ size }: P) => (<svg {...S(size)}><path d="M21 12a9 9 0 1 1-2.64-6.36" /><path d="M21 3v6h-6" /></svg>);
export const IcPrinter = ({ size }: P) => (<svg {...S(size)}><path d="M6 9V3h12v6" /><path d="M6 18H4a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2h-2" /><rect x="6" y="14" width="12" height="7" rx="1" /></svg>);
export const IcChevron = ({ size, className }: P & { className?: string }) => (<svg {...S(size)} className={className}><path d="m6 9 6 6 6-6" /></svg>);
export const IcStar = ({ size, filled }: P & { filled?: boolean }) => (
  <svg {...S(size)} fill={filled ? "currentColor" : "none"}>
    <path d="m12 3.5 2.6 5.3 5.9.9-4.2 4.1 1 5.8-5.3-2.8-5.3 2.8 1-5.8-4.2-4.1 5.9-.9z" />
  </svg>
);

export const IcHome = ({ size }: P) => (<svg {...S(size)}><path d="M3 10.5 12 3l9 7.5" /><path d="M5 9.5V21h14V9.5" /></svg>);
export const IcGlobe = ({ size }: P) => (<svg {...S(size)}><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.5 3 2.5 15 0 18M12 3c-2.5 3-2.5 15 0 18" /></svg>);
export const IcUsers = ({ size }: P) => (<svg {...S(size)}><circle cx="9" cy="8" r="3.2" /><path d="M3 20c0-3.3 2.7-6 6-6s6 2.7 6 6" /><path d="M16 5.2A3.2 3.2 0 0 1 16 11M21 20c0-2.5-1.3-4.6-3.3-5.5" /></svg>);
export const IcShield = ({ size }: P) => (<svg {...S(size)}><path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6Z" /><path d="m9 12 2 2 4-4" /></svg>);
export const IcCard = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="5" width="18" height="14" rx="2.5" /><path d="M3 10h18" /></svg>);
export const IcChat = ({ size }: P) => (<svg {...S(size)}><path d="M21 12a8 8 0 0 1-11.5 7.2L4 20l.8-4.5A8 8 0 1 1 21 12Z" /><path d="M8.5 11h7M8.5 14h4" /></svg>);
export const IcPhone = ({ size }: P) => (<svg {...S(size)}><path d="M6.5 4h3l1.3 4-2 1.4a11 11 0 0 0 5.3 5.3l1.4-2 4 1.3v3a2 2 0 0 1-2.2 2A16 16 0 0 1 4.5 6.2 2 2 0 0 1 6.5 4Z" /></svg>);
export const IcMail = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="5" width="18" height="14" rx="2.5" /><path d="m4 7 8 6 8-6" /></svg>);
export const IcGear = ({ size }: P) => (<svg {...S(size)}><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-2.9 1.2V21a2 2 0 0 1-4 0v-.1a1.7 1.7 0 0 0-2.9-1.2l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0-1.2-2.9H3a2 2 0 0 1 0-4h.1a1.7 1.7 0 0 0 1.2-2.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 0 1 4 0v.1a1.7 1.7 0 0 0 2.9 1.2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 0 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z" /></svg>);
export const IcPlus = ({ size }: P) => (<svg {...S(size)}><path d="M12 5v14M5 12h14" /></svg>);
export const IcKey = ({ size }: P) => (<svg {...S(size)}><circle cx="8" cy="15" r="4" /><path d="m10.8 12.2 8.2-8.2M17 5l2 2M15 7l2 2" /></svg>);
export const IcTrash = ({ size }: P) => (<svg {...S(size)}><path d="M3 6h18M8 6V4h8v2M6 6v14a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V6" /></svg>);
export const IcCopy = ({ size }: P) => (<svg {...S(size)}><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M6 15H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v1" /></svg>);
export const IcPresent = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4" width="18" height="12" rx="1.5" /><path d="M12 16v4M8.5 20h7" /><path d="M7 11l2.5-2.5 2 2L16 7" /></svg>);
export const IcLogout = ({ size }: P) => (<svg {...S(size)}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" /></svg>);
export const IcExternal = ({ size }: P) => (<svg {...S(size)}><path d="M15 3h6v6M10 14 21 3M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5" /></svg>);
// Maximize (corners pointing OUT) and restore/back to normal (corners pointing IN) — like Gmail's
// compose window "full screen". Two distinct icons for the two states.
export const IcMaximize = ({ size }: P) => (<svg {...S(size)}><path d="M8 3H5a2 2 0 0 0-2 2v3M21 8V5a2 2 0 0 0-2-2h-3M3 16v3a2 2 0 0 0 2 2h3M16 21h3a2 2 0 0 0 2-2v-3" /></svg>);
export const IcRestoreWin = ({ size }: P) => (<svg {...S(size)}><path d="M8 3v3a2 2 0 0 1-2 2H3M21 8h-3a2 2 0 0 1-2-2V3M3 16h3a2 2 0 0 1 2 2v3M16 21v-3a2 2 0 0 1 2-2h3" /></svg>);
export const IcCheck = ({ size }: P) => (<svg {...S(size)}><path d="m5 12 5 5L20 7" /></svg>);
export const IcX = ({ size }: P) => (<svg {...S(size)}><path d="M6 6l12 12M18 6 6 18" /></svg>);
export const IcMoon = ({ size }: P) => (<svg {...S(size)}><path d="M21 12.8A8 8 0 1 1 11.2 3 6.4 6.4 0 0 0 21 12.8Z" /></svg>);
export const IcFolder = ({ size }: P) => (<svg {...S(size)}><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /></svg>);
export const IcArchive = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4" width="18" height="4" rx="1" /><path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8" /><path d="M10 12h4" /></svg>);
export const IcBook = ({ size }: P) => (<svg {...S(size)}><path d="M4 5a2 2 0 0 1 2-2h13v16H6a2 2 0 0 0-2 2Z" /><path d="M4 19a2 2 0 0 0 2 2h13" /><path d="M9 7h6" /><path d="M9 11h6" /></svg>);
// Documents: a page with a folded corner and lines. Distinct from IcBook (the wiki) and from
// IcPencilBox (notes) at the 20px the rail uses, which is the only size that matters here.
export const IcDoc = ({ size }: P) => (<svg {...S(size)}><path d="M6 3.5h7.5L18 8v12.5H6Z" /><path d="M13.5 3.5V8H18" /><path d="M9 12h6" /><path d="M9 15.5h6" /></svg>);
export const IcMicOff = ({ size }: P) => (<svg {...S(size)}><path d="M1 1l22 22" /><path d="M9 9v3a3 3 0 0 0 5.1 2.1" /><path d="M15 9.3V5a3 3 0 0 0-5.9-.6" /><path d="M17 16.9A7 7 0 0 1 5 12v-2" /><path d="M19 10v2a7 7 0 0 1-.1 1.2" /><path d="M12 19v3" /></svg>);
export const IcHandRaise = ({ size }: P) => (<svg {...S(size)}><path d="M18 11V6a1.5 1.5 0 0 0-3 0" /><path d="M15 6V4.5a1.5 1.5 0 0 0-3 0V6" /><path d="M12 6V5a1.5 1.5 0 0 0-3 0v7" /><path d="M9 8.5a1.5 1.5 0 0 0-3 0v5.5a7 7 0 0 0 7 7h1a6 6 0 0 0 6-6v-4" /></svg>);
export const IcEmoji = ({ size }: P) => (<svg {...S(size)}><circle cx="12" cy="12" r="9" /><path d="M8 14a4.5 4.5 0 0 0 8 0" /><path d="M9 9h.01" /><path d="M15 9h.01" /></svg>);
export const IcClock = ({ size }: P) => (<svg {...S(size)}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></svg>);
export const IcSun = ({ size }: P) => (<svg {...S(size)}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>);
export const IcColumns = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4" width="5.5" height="16" rx="1.2" /><rect x="9.5" y="4" width="5.5" height="11" rx="1.2" /><rect x="16" y="4" width="5" height="14" rx="1.2" /></svg>);
export const IcCalendar = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4.5" width="18" height="16" rx="2" /><path d="M16 3v3M8 3v3M3 9.5h18" /></svg>);
export const IcSearch = ({ size }: P) => (<svg {...S(size)}><circle cx="11" cy="11" r="7" /><path d="m21 21-4-4" /></svg>);
export const IcPencil = ({ size }: P) => (<svg {...S(size)}><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>);
export const IcLock = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="11" width="18" height="11" rx="2" /><path d="M7 11V7a5 5 0 0 1 10 0v4" /></svg>);
export const IcArrowLeft = ({ size }: P) => (<svg {...S(size)}><path d="M19 12H5" /><path d="m12 19-7-7 7-7" /></svg>);
export const IcReplyAll = ({ size }: P) => (<svg {...S(size)}><path d="m7 17-5-5 5-5" /><path d="m12 17-5-5 5-5" /><path d="M22 18v-2a4 4 0 0 0-4-4H9" /></svg>);
export const IcForward = ({ size }: P) => (<svg {...S(size)}><path d="m15 17 5-5-5-5" /><path d="M4 18v-2a4 4 0 0 1 4-4h12" /></svg>);
export const IcReply = ({ size }: P) => (<svg {...S(size)}><path d="M9 17 4 12l5-5" /><path d="M20 18v-2a4 4 0 0 0-4-4H4" /></svg>);
export const IcSparkle = ({ size }: P) => (<svg {...S(size)}><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8Z" /></svg>);
export const IcUpload = ({ size }: P) => (<svg {...S(size)}><path d="M12 15V4M8 8l4-4 4 4M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" /></svg>);
export const IcBell = ({ size }: P) => (<svg {...S(size)}><path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" /><path d="M13.7 21a2 2 0 0 1-3.4 0" /></svg>);
export const IcSend = ({ size }: P) => (<svg {...S(size)}><path d="M22 2 11 13" /><path d="M22 2 15 22l-4-9-9-4Z" /></svg>);
export const IcPaperclip = ({ size }: P) => (<svg {...S(size)}><path d="m21.4 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" /></svg>);
export const IcGrid = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></svg>);
export const IcList = ({ size }: P) => (<svg {...S(size)}><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" /></svg>);
export const IcShare = ({ size }: P) => (<svg {...S(size)}><circle cx="18" cy="5" r="3" /><circle cx="6" cy="12" r="3" /><circle cx="18" cy="19" r="3" /><path d="m8.6 13.5 6.8 4M15.4 6.5l-6.8 4" /></svg>);
export const IcDownload = ({ size }: P) => (<svg {...S(size)}><path d="M12 3v12M8 11l4 4 4-4M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2" /></svg>);
export const IcFolderPlus = ({ size }: P) => (<svg {...S(size)}><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /><path d="M12 11v4M10 13h4" /></svg>);
export const IcLink = ({ size }: P) => (<svg {...S(size)}><path d="M10 13a5 5 0 0 0 7 0l2-2a5 5 0 0 0-7-7l-1 1" /><path d="M14 11a5 5 0 0 0-7 0l-2 2a5 5 0 0 0 7 7l1-1" /></svg>);
export const IcImage = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="8.5" cy="8.5" r="1.5" /><path d="m21 15-5-5L5 21" /></svg>);
export const IcPin = ({ size }: P) => (<svg {...S(size)}><path d="M12 17v5" /><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" /></svg>);
export const IcPencilBox = ({ size }: P) => (<svg {...S(size)}><path d="M19 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h6" /><path d="M17.5 3.5a2.1 2.1 0 0 1 3 3L12 15l-4 1 1-4Z" /></svg>);
export const IcVideo = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="6" width="13" height="12" rx="2.5" /><path d="m16 10 5-3v10l-5-3z" /></svg>);

// --- Text formatting (chat editor toolbar, Slack-style) ---
// Strokes instead of typography: the old letters (B/I/S) changed weight with the system font and
// didn't match the rest of the icons. Here it's all the same outline drawing, inheriting the color.
export const IcBold = ({ size }: P) => (<svg {...S(size)}><path d="M7 5h6a3.5 3.5 0 0 1 0 7H7Z" /><path d="M7 12h7a3.5 3.5 0 0 1 0 7H7Z" /></svg>);
export const IcItalic = ({ size }: P) => (<svg {...S(size)}><path d="M15 5h-5M14 19H9M13.5 5 10.5 19" /></svg>);
export const IcUnderline = ({ size }: P) => (<svg {...S(size)}><path d="M7 4v6a5 5 0 0 0 10 0V4" /><path d="M6 20h12" /></svg>);
export const IcStrike = ({ size }: P) => (<svg {...S(size)}><path d="M4 12h16" /><path d="M16.5 7a4 4 0 0 0-4-2.5c-2.5 0-4 1.3-4 3.2 0 1.4.9 2.4 2.5 3" /><path d="M8 16.5A4.2 4.2 0 0 0 12 19c2.6 0 4.2-1.3 4.2-3.2 0-1-.4-1.8-1.2-2.4" /></svg>);
export const IcListOrdered = ({ size }: P) => (<svg {...S(size)}><path d="M10 6h11M10 12h11M10 18h11" /><path d="M4 5.5 5.2 5v4M3.6 15.2c0-.7.6-1.2 1.3-1.2s1.3.5 1.3 1.2c0 1.2-2.6 1.6-2.6 3.3h2.7" /></svg>);
export const IcQuote = ({ size }: P) => (<svg {...S(size)}><path d="M4 5v14" /><path d="M9 8h11M9 12h11M9 16h7" /></svg>);
export const IcCode = ({ size }: P) => (<svg {...S(size)}><path d="m9 8-5 4 5 4M15 8l5 4-5 4" /></svg>);
export const IcCodeBlock = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4.5" width="18" height="15" rx="2" /><path d="m9.5 10-2 2 2 2M14.5 10l2 2-2 2" /></svg>);
export const IcAt = ({ size }: P) => (<svg {...S(size)}><circle cx="12" cy="12" r="4" /><path d="M16 8v5a2.6 2.6 0 0 0 5 0v-1a9 9 0 1 0-3.5 7.1" /></svg>);
export const IcSlash = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4" width="18" height="16" rx="3" /><path d="m14 8.5-4 7" /></svg>);
// "Aa": shows/hides the formatting toolbar (same button as Slack's).
export const IcTextSize = ({ size }: P) => (<svg {...S(size)}><path d="M2.5 18 7 7l4.5 11M4 14.5h6" /><circle cx="16.6" cy="15.4" r="3.1" /><path d="M19.7 12.3v6.3" /></svg>);
export const IcTable = ({ size }: P) => (<svg {...S(size)}><rect x="3" y="4.5" width="18" height="15" rx="2" /><path d="M3 9.5h18M9 9.5V19.5M15 9.5V19.5" /></svg>);
