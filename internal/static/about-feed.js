export async function loadCaremePosts(fetchPage) {
  const response = await fetchPage("/updates/feed.json");
  if (!response.ok) throw new Error(`Local feed returned ${response.status}`);
  return response.json();
}

export function textParts(text) {
  return text.split(/(https?:\/\/[^\s]+)/g).filter(Boolean).map(part =>
    /^https?:\/\//.test(part) ? { text: part, href: part } : { text: part });
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

function renderPost(post) {
  const card = element("article", "min-w-0 overflow-hidden rounded-xl border border-brand-100 bg-white");
  const body = element("div", "p-6");
  const date = element("time", "text-sm text-gray-500",
    new Date(post.date).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" }));
  date.dateTime = post.date;
  body.append(date);
  const text = element("p", "mt-3 whitespace-pre-wrap break-words leading-7 text-gray-700");
  for (const part of textParts(post.text)) {
    text.append(part.href
      ? externalLink(part.href, "text-brand-700 underline underline-offset-4", part.text)
      : document.createTextNode(part.text));
  }
  body.append(text);
  card.append(body);
  const images = post.media.filter(media => media.type.startsWith("image/"));
  const gallery = element("div", "grid grid-cols-2 gap-1 sm:flex");
  for (const [index, image] of images.entries()) {
    const lastOddImage = images.length % 2 === 1 && index === images.length - 1;
    const link = externalLink("/updates/" + image.src, lastOddImage
      ? "block min-w-0 col-span-2 sm:flex-1" : "block min-w-0 sm:flex-1");
    link.setAttribute("aria-label", image.alt || "View full photo");
    const img = element("img", "aspect-[4/3] max-h-80 w-full object-cover");
    img.src = "/updates/" + image.src;
    img.alt = image.alt;
    img.loading = "lazy";
    link.append(img);
    gallery.append(link);
  }
  if (images.length) card.append(gallery);
  for (const media of post.media.filter(media => media.type.startsWith("video/"))) {
    const video = element("video", "block max-h-[28rem] w-full bg-black");
    video.controls = true;
    video.playsInline = true;
    video.preload = "metadata";
    const source = element("source", "");
    source.src = "/updates/" + media.src;
    source.type = media.type;
    video.append(source);
    video.setAttribute("aria-label", "Careme cooking video");
    const error = element("p", "p-4 text-sm text-gray-600");
    error.hidden = true;
    error.setAttribute("role", "status");
    video.addEventListener("error", () => {
      error.textContent = "The video couldn’t load. Try refreshing the page, chef.";
      error.hidden = false;
    });
    source.addEventListener("error", () => {
      error.textContent = "The video couldn’t load. Try refreshing the page, chef.";
      error.hidden = false;
    });
    card.append(video, error);
  }
  return card;
}

export function mountFeed(posts, { feed, status, controls, previous, next, position }) {
  let index = 0;
  const showPost = () => {
    for (const video of feed.querySelectorAll("video")) video.pause();
    feed.replaceChildren(renderPost(posts[index]));
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
