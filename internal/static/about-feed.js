const actor = "did:plc:qv4hafijvatyb773lcm6c6ru";

function webURL(value) {
  try {
    const url = new URL(value);
    return ["https:", "http:"].includes(url.protocol) ? url.href : null;
  } catch {
    return null;
  }
}

function linksIn(record) {
  return (record.facets ?? []).flatMap(facet =>
    facet.features.filter(feature => feature.$type === "app.bsky.richtext.facet#link")
      .map(feature => feature.uri));
}

export function isCaremePost(item) {
  const { post } = item;
  if (item.reason || post.author.did !== actor || post.record.reply) return false;
  // Require a real Careme link, not a mention in a personal post or a quoted post.
  const links = [...linksIn(post.record), post.record.embed?.external?.uri];
  return links.some(link => {
    const url = webURL(link);
    return url && ["careme.cooking", "www.careme.cooking"].includes(new URL(url).hostname);
  });
}

export async function loadCaremePosts(fetchPage, limit = 4) {
  const posts = new Map();
  let cursor;
  // Bound the work even if the account has a long stretch of personal posts.
  for (let page = 0; page < 5; page++) {
    const url = new URL("https://public.api.bsky.app/xrpc/app.bsky.feed.getAuthorFeed");
    url.searchParams.set("actor", actor);
    url.searchParams.set("filter", "posts_no_replies");
    url.searchParams.set("limit", "100");
    if (cursor) url.searchParams.set("cursor", cursor);
    const response = await fetchPage(url);
    if (!response.ok) throw new Error(`Bluesky feed returned ${response.status}`);
    const data = await response.json();
    for (const item of data.feed) {
      if (isCaremePost(item)) posts.set(item.post.uri, item.post);
    }
    if (posts.size >= limit || !data.cursor || data.cursor === cursor) break;
    cursor = data.cursor;
  }
  return [...posts.values()].sort((a, b) =>
    Date.parse(b.record.createdAt) - Date.parse(a.record.createdAt)).slice(0, limit);
}

// Bluesky facet offsets count UTF-8 bytes, rather than JavaScript characters.
export function textParts(record) {
  const bytes = new globalThis.TextEncoder().encode(record.text);
  const decoder = new globalThis.TextDecoder();
  const parts = [];
  let offset = 0;
  for (const facet of [...(record.facets ?? [])].sort((a, b) => a.index.byteStart - b.index.byteStart)) {
    const link = facet.features.find(feature => feature.$type === "app.bsky.richtext.facet#link");
    const href = link && webURL(link.uri);
    if (!href) continue;
    const { byteStart, byteEnd } = facet.index;
    parts.push({ text: decoder.decode(bytes.slice(offset, byteStart)) });
    parts.push({ text: decoder.decode(bytes.slice(byteStart, byteEnd)), href });
    offset = byteEnd;
  }
  parts.push({ text: decoder.decode(bytes.slice(offset)) });
  return parts;
}

function element(tag, className, text) {
  const node = document.createElement(tag);
  node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function externalLink(href, className, text) {
  const link = element("a", className, text);
  link.href = href;
  link.target = "_blank";
  link.rel = "noreferrer";
  return link;
}

// Bluesky serves video as HLS. Load the player only for a visible video post.
export function attachVideo(video, playlist, onError, loadHls = () => import("https://cdn.jsdelivr.net/npm/hls.js@1.7.4/dist/hls.mjs")) {
  let disposed = false;
  let player;
  const failed = () => { if (!disposed) onError(); };
  video.addEventListener("error", failed);
  const ready = (async () => {
    if (video.canPlayType("application/vnd.apple.mpegurl") && "ManagedMediaSource" in window) {
      video.src = playlist;
      return;
    }
    const { default: Hls } = await loadHls();
    if (disposed) return;
    if (Hls.isSupported()) {
      player = new Hls({ enableWorker: false });
      player.on(Hls.Events.ERROR, (_event, data) => { if (data.fatal) failed(); });
      player.loadSource(playlist);
      player.attachMedia(video);
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = playlist;
    } else {
      failed();
    }
  })().catch(failed);
  return {
    ready,
    dispose() {
      disposed = true;
      video.removeEventListener("error", failed);
      video.pause();
      player?.destroy();
      video.removeAttribute("src");
      video.load();
    },
  };
}

function renderPost(post) {
  const card = element("article", "min-w-0 overflow-hidden rounded-xl border border-brand-100 bg-white");
  const postURL = `https://bsky.app/profile/${actor}/post/${post.uri.split("/").pop()}`;
  const body = element("div", "p-6");
  const date = element("time", "text-sm text-gray-500",
    new Date(post.record.createdAt).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" }));
  date.dateTime = post.record.createdAt;
  body.append(date);
  const text = element("p", "mt-3 whitespace-pre-wrap break-words leading-7 text-gray-700");
  for (const part of textParts(post.record)) {
    text.append(part.href
      ? externalLink(part.href, "text-brand-700 underline underline-offset-4", part.text)
      : document.createTextNode(part.text));
  }
  body.append(text);
  body.append(externalLink(postURL, "mt-4 inline-block text-sm font-semibold text-brand-700 underline underline-offset-4", "View post on Bluesky"));
  card.append(body);
  const media = post.embed?.media ?? post.embed;
  if (media?.playlist) {
    const video = element("video", "block max-h-[28rem] w-full bg-black");
    video.controls = true;
    video.playsInline = true;
    video.preload = "metadata";
    video.poster = webURL(media.thumbnail) ?? "";
    video.dataset.playlist = webURL(media.playlist);
    video.setAttribute("aria-label", post.record.embed?.alt || "Careme cooking video");
    const error = element("p", "p-4 text-sm text-gray-600");
    error.hidden = true;
    error.setAttribute("role", "status");
    error.dataset.videoError = "";
    card.append(video, error);
  } else if (media?.images?.length) {
    const gallery = element("div", media.images.length > 2 ? "grid grid-cols-2 gap-1 sm:grid-cols-4" : "grid grid-cols-2 gap-1");
    if (media.images.length === 1) gallery.className = "block";
    for (const image of media.images) {
      const source = webURL(image.fullsize);
      if (!source) continue;
      const link = externalLink(source, "block", undefined);
      link.setAttribute("aria-label", image.alt || "View full photo");
      const img = element("img", "aspect-[4/3] max-h-80 w-full object-cover");
      img.src = source;
      img.alt = image.alt;
      img.loading = "lazy";
      img.width = image.aspectRatio?.width ?? 1000;
      img.height = image.aspectRatio?.height ?? 1000;
      link.append(img);
      gallery.append(link);
    }
    card.append(gallery);
  }
  return card;
}

export function mountFeed(posts, { feed, status, controls, previous, next, position }) {
  let index = 0;
  let activeVideo;
  const showPost = () => {
    activeVideo?.dispose();
    activeVideo = undefined;
    const card = renderPost(posts[index]);
    feed.replaceChildren(card);
    const video = card.querySelector("video");
    if (video) {
      activeVideo = attachVideo(video, video.dataset.playlist, () => {
        const error = card.querySelector("[data-video-error]");
        error.textContent = "The video couldn’t load. Use ‘View post on Bluesky’ to watch it there.";
        error.hidden = false;
      });
    }
    position.textContent = `Post ${index + 1} of ${posts.length}`;
    previous.disabled = index === 0;
    next.disabled = index === posts.length - 1;
  };
  previous.addEventListener("click", () => {
    if (index > 0) { index--; showPost(); }
  });
  next.addEventListener("click", () => {
    if (index < posts.length - 1) { index++; showPost(); }
  });
  if (posts.length) showPost();
  status.textContent = posts.length ? "" : "No Careme posts to show yet.";
  controls.hidden = posts.length < 2;
}

async function initializeFeed() {
  const feed = document.getElementById("careme-feed");
  const status = document.getElementById("careme-feed-status");
  try {
    const posts = await loadCaremePosts(url => window.fetch(url, { signal: window.AbortSignal.timeout(15000) }));
    mountFeed(posts, {
      feed, status,
      controls: document.getElementById("careme-feed-controls"),
      previous: document.getElementById("careme-feed-previous"),
      next: document.getElementById("careme-feed-next"),
      position: document.getElementById("careme-feed-position"),
    });
  } catch {
    status.textContent = "The latest posts couldn’t load. Try refreshing the page, chef, or visit Careme on Facebook below.";
  } finally {
    feed.setAttribute("aria-busy", "false");
  }
}

if (typeof document !== "undefined") initializeFeed();
