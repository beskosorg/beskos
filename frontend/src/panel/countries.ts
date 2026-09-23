// Countries for the billing form. Codes only: the NAME comes from Intl.DisplayNames in the
// viewer's own language — translating 200 countries by hand would be wasted work and go stale.

/** The 27 European Union members (decide the VAT rule). */
export const EU_COUNTRIES = [
  "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR", "HU",
  "IE", "IT", "LV", "LT", "LU", "MT", "NL", "PL", "PT", "RO", "SK", "SI", "ES", "SE",
];

/** Outside the EU: markets where there are already customers or where requests come from. */
const NON_EU = [
  "GB", "CH", "NO", "IS", "UA", "US", "CA", "BR", "MX", "AR", "CL", "CO", "AO", "MZ",
  "CV", "ZA", "AE", "IL", "TR", "IN", "AU", "NZ", "JP", "SG",
];

/** Full selector list: the EU first (where the VAT rule bites), then the rest. */
export const COUNTRIES = [...EU_COUNTRIES, ...NON_EU];
