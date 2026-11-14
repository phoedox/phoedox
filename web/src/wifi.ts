/** Escapes a value for a Wi-Fi QR payload (\ ; , : " are escaped). */
export const wifiEscape = (s: string) => s.replace(/([\;,:"])/g, "\\$1");

/** The text of a Wi-Fi QR code (WIFI:T:…;S:…;P:…;;), understood by phone cameras. */
export const wifiPayload = (ssid: string, password: string, security: string, hidden: boolean) =>
  `WIFI:T:${security === "nopass" ? "nopass" : security};S:${wifiEscape(ssid)};${security === "nopass" ? "" : `P:${wifiEscape(password)};`}${hidden ? "H:true;" : ""};`;
