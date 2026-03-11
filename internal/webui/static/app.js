const state = {
  sessionId: null,
  source: null,
  hops: new Map(),
  markers: [],
  polylines: [],
  selectedPopup: null,
};

const map = L.map("map", { worldCopyJump: true }).setView([0, 0], 2);
L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
  attribution: "&copy; OpenStreetMap contributors",
}).addTo(map);

const form = document.getElementById("trace-form");
const targetInput = document.getElementById("target");
const protocolInput = document.getElementById("protocol");
const dataProviderInput = document.getElementById("data-provider");
const dnsResolverInput = document.getElementById("dns-resolver");
const timeoutInput = document.getElementById("timeout");
const mtrInput = document.getElementById("mtr");
const startButton = document.getElementById("start-button");
const stopButton = document.getElementById("stop-button");
const statusBadge = document.getElementById("app-status-badge");
const hopCountNode = document.getElementById("hop-count");
const replyCountNode = document.getElementById("reply-count");
const mapNodeCountNode = document.getElementById("map-node-count");
const mapEndpointNode = document.getElementById("map-endpoint");
const tableBody = document.getElementById("hop-table-body");

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  await startTrace();
});

stopButton.addEventListener("click", async () => {
  if (!state.sessionId) {
    return;
  }
  await fetch(`/api/traces/${state.sessionId}`, { method: "DELETE" });
});

async function startTrace() {
  if (state.sessionId) {
    await fetch(`/api/traces/${state.sessionId}`, { method: "DELETE" }).catch(() => {});
  }
  closeStream();
  resetUI();

  const payload = {
    target: targetInput.value.trim(),
    protocol: protocolInput.value,
    dataProvider: dataProviderInput.value,
    dnsResolver: dnsResolverInput.value,
    language: detectBrowserLanguage(),
    timeoutSeconds: Number(timeoutInput.value),
    mtr: mtrInput.checked,
  };

  startButton.disabled = true;
  setStatus("running");

  try {
    const response = await fetch("/api/traces", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }

    const result = await response.json();
    state.sessionId = result.id;
    setStatus(result.status);
    stopButton.disabled = false;

    const source = new EventSource(result.streamUrl);
    state.source = source;
    source.onmessage = (message) => {
      const event = JSON.parse(message.data);
      handleEvent(event);
    };
    source.onerror = () => {
      closeStream();
      if (state.sessionId && startButton.disabled) {
        setStatus("error");
      }
      stopButton.disabled = true;
      startButton.disabled = false;
    };
  } catch (error) {
    setStatus("error", error instanceof Error ? error.message : String(error));
    startButton.disabled = false;
  }
}

function handleEvent(event) {
  switch (event.type) {
    case "hop":
      upsertHop(event.hop);
      renderHops();
      redrawMap();
      break;
    case "status":
      setStatus(event.status, event.message);
      if (event.status !== "running") {
        closeStream();
        stopButton.disabled = true;
        startButton.disabled = false;
      }
      break;
    case "error":
    case "log":
      break;
    default:
      break;
  }
}

function upsertHop(hop) {
  const existing = state.hops.get(hop.no);
  if (!existing) {
    state.hops.set(hop.no, createAggregateHop(hop));
  } else {
    existing.samples.push(hop);
  }
  hopCountNode.textContent = `${state.hops.size} 跳`;
}

function renderHops() {
  const hops = [...state.hops.values()].sort((a, b) => a.no - b.no);
  if (hops.length === 0) {
    tableBody.innerHTML = '<tr class="empty-row"><td colspan="7">暂无跃点数据。</td></tr>';
    return;
  }

  tableBody.innerHTML = hops.map((hop) => `
    <tr class="hop-row" data-hop="${hop.no}">
      <td><span class="hop-pill">${hop.no}</span></td>
      <td>${escapeHTML(formatHopIPs(hop))}</td>
      <td>${escapeHTML(formatHopTimes(hop))}</td>
      <td>${escapeHTML(formatHopGeolocation(hop))}</td>
      <td>${escapeHTML(formatHopOrganization(hop))}</td>
      <td>${escapeHTML(formatHopAS(hop))}</td>
      <td>${escapeHTML(formatHopHostname(hop))}</td>
    </tr>
  `).join("");

  document.querySelectorAll(".hop-row").forEach((row) => {
    row.addEventListener("click", () => {
      const hopNo = Number(row.dataset.hop);
      focusHop(hopNo);
    });
  });
}

function redrawMap() {
  state.markers.forEach((marker) => map.removeLayer(marker));
  state.markers = [];
  state.polylines.forEach((polyline) => map.removeLayer(polyline));
  state.polylines = [];

  const hops = [...state.hops.values()]
    .sort((a, b) => a.no - b.no)
    .map(buildMapHop)
    .filter(Boolean)
    .map((hop) => ({
      ...hop,
      lat: Number.parseFloat(hop.sample.latitude),
      lng: Number.parseFloat(hop.sample.longitude),
    }))
    .filter((hop) => Number.isFinite(hop.lat) && Number.isFinite(hop.lng));

  const route = buildRoutePath(hops);
  updateRouteSummary(hops);

  for (const point of route.points) {
    const latLng = point.latLng;
    const marker = L.circleMarker(latLng, {
      radius: 8,
      fillColor: "#0f8b8d",
      color: "#005f73",
      weight: 2,
      opacity: 1,
      fillOpacity: 0.9,
    }).addTo(map);

    marker.bindPopup(renderPopup(point.hop.aggregate));
    marker.on("mouseover", () => marker.openPopup());
    state.markers.push(marker);
  }

  if (route.points.length > 0) {
    const polyline = L.polyline(route.points.map((point) => point.latLng), {
      color: "#bc6c25",
      opacity: 0.85,
      weight: 3,
    }).addTo(map);
    state.polylines.push(polyline);
  }

  if (route.points.length > 1) {
    map.fitBounds(route.points.map((point) => point.latLng), { padding: [24, 24] });
  } else if (route.points.length === 1) {
    map.setView(route.points[0].latLng, 6);
  }
}

function focusHop(hopNo) {
  const hop = buildMapHop(state.hops.get(hopNo));
  if (!hop) {
    return;
  }
  const sample = hop.sample;
  if (!sample.latitude || !sample.longitude) {
    return;
  }

  const lat = Number.parseFloat(sample.latitude);
  const lng = Number.parseFloat(sample.longitude);
  if (!Number.isFinite(lat) || !Number.isFinite(lng)) {
    return;
  }

  map.setView([lat, lng], 7);
}

function renderPopup(hop) {
  return `
    <strong>#${hop.no}</strong><br>
    ${escapeHTML(formatHopIPs(hop))}<br>
    ${escapeHTML(formatHopTimes(hop))}<br>
    ${escapeHTML(formatHopGeolocation(hop))}<br>
    ${escapeHTML(formatHopOrganization(hop))}<br>
    ${escapeHTML(formatHopAS(hop))}
  `;
}

function resetUI() {
  state.sessionId = null;
  state.hops.clear();
  renderHops();
  redrawMap();
  hopCountNode.textContent = "0 跳";
  replyCountNode.textContent = "0 个响应";
  mapNodeCountNode.textContent = "0 个节点";
  mapEndpointNode.textContent = "暂无终点";
  setStatus("idle");
  stopButton.disabled = true;
  startButton.disabled = false;
}

function setStatus(status, message = "") {
  if (!statusBadge) {
    return;
  }
  statusBadge.textContent = formatStatus(status);
  statusBadge.className = `topbar-badge ${status}`;
  if (message) {
    statusBadge.title = message;
  } else {
    statusBadge.removeAttribute("title");
  }
}

function closeStream() {
  if (state.source) {
    state.source.close();
    state.source = null;
  }
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function createAggregateHop(hop) {
  return {
    no: hop.no,
    samples: [hop],
  };
}

function uniqueNonEmpty(values, fallback = "") {
  const unique = [];
  for (const value of values) {
    const normalized = String(value || "").trim();
    if (!normalized) {
      continue;
    }
    if (normalized === "*" && fallback !== "*") {
      continue;
    }
    if (!unique.includes(normalized)) {
      unique.push(normalized);
    }
  }
  if (unique.length === 0 && fallback) {
    return [fallback];
  }
  return unique;
}

function formatHopIPs(hop) {
  return uniqueNonEmpty(hop.samples.map((sample) => sample.ip), "*").join("\n");
}

function formatHopTimes(hop) {
  const times = hop.samples.map((sample) => sample.time || "");
  return times.length > 0 ? times.join(" / ") : "";
}

function formatHopGeolocation(hop) {
  return uniqueNonEmpty(hop.samples.map((sample) => sample.geolocation)).join("\n");
}

function formatHopOrganization(hop) {
  return uniqueNonEmpty(hop.samples.map((sample) => sample.organization)).join("\n");
}

function formatHopAS(hop) {
  return uniqueNonEmpty(hop.samples.map((sample) => sample.as)).join("\n");
}

function formatHopHostname(hop) {
  return uniqueNonEmpty(hop.samples.map((sample) => sample.hostname)).join("\n");
}

function buildMapHop(hop) {
  if (!hop || !hop.samples) {
    return null;
  }
  for (let index = hop.samples.length - 1; index >= 0; index -= 1) {
    const sample = hop.samples[index];
    if (sample.latitude && sample.longitude && sample.latitude !== "0" && sample.longitude !== "0") {
      return {
        no: hop.no,
        aggregate: hop,
        sample,
      };
    }
  }
  return null;
}

function buildRoutePath(hops) {
  if (hops.length === 0) {
    return { points: [] };
  }

  const points = [];
  let previousAdjustedLng = null;

  for (const hop of hops) {
    const adjustedLng = adjustLongitude(hop.lng, previousAdjustedLng);
    points.push({
      hop,
      latLng: [hop.lat, adjustedLng],
    });
    previousAdjustedLng = adjustedLng;
  }

  return { points };
}

function adjustLongitude(lng, previousAdjustedLng) {
  if (previousAdjustedLng === null) {
    return lng;
  }

  let adjustedLng = lng;
  const diff = adjustedLng - previousAdjustedLng;
  if (diff >= 180) {
    adjustedLng -= 360;
  } else if (diff <= -180) {
    adjustedLng += 360;
  }
  return adjustedLng;
}

function updateRouteSummary(mapHops) {
  const responsiveHops = [...state.hops.values()].filter((hop) =>
    hop.samples.some((sample) => isResponsiveSample(sample)),
  ).length;
  replyCountNode.textContent = `${responsiveHops} 个响应`;
  mapNodeCountNode.textContent = `${mapHops.length} 个节点`;
  mapEndpointNode.textContent = mapHops.length > 0
    ? formatEndpoint(mapHops[mapHops.length - 1].aggregate)
    : "暂无终点";
}

function formatStatus(status) {
  switch (status) {
    case "running":
      return "进行中";
    case "completed":
      return "已完成";
    case "stopped":
      return "已停止";
    case "timeout":
      return "已超时";
    case "failed":
      return "失败";
    case "error":
      return "错误";
    case "":
    case null:
    case undefined:
    default:
      return "空闲";
  }
}

function isResponsiveSample(sample) {
  return Boolean(
    (sample.ip && sample.ip !== "*") ||
    (sample.time && sample.time !== "*"),
  );
}

function formatEndpoint(hop) {
  const hostnames = uniqueNonEmpty(hop.samples.map((sample) => sample.hostname));
  if (hostnames.length > 0) {
    return hostnames[hostnames.length - 1];
  }

  const ips = uniqueNonEmpty(hop.samples.map((sample) => sample.ip));
  if (ips.length > 0) {
    return ips[ips.length - 1];
  }

  return `第 ${hop.no} 跳`;
}

function detectBrowserLanguage() {
  if (Array.isArray(navigator.languages) && navigator.languages.length > 0) {
    const preferredChinese = navigator.languages.find((language) =>
      String(language || "").toLowerCase().startsWith("zh"),
    );
    if (preferredChinese) {
      return preferredChinese;
    }
    return navigator.languages[0];
  }
  return navigator.language || "en";
}
