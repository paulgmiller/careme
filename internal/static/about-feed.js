const feed = document.getElementById("careme-feed");
const status = document.getElementById("careme-feed-status");
const controls = document.getElementById("careme-feed-controls");
const newer = document.getElementById("careme-feed-newer");
const older = document.getElementById("careme-feed-older");
const clone = id => document.getElementById(id).content.firstElementChild.cloneNode(true);

async function initializeFeed() {
  try {
    const response = await window.fetch("/updates/feed.json", { signal: window.AbortSignal.timeout(15000) });
    if (!response.ok) throw new Error(`Feed returned ${response.status}`);
    const posts = await response.json();
    if (!posts.length) {
      status.textContent = "No Careme posts to show yet.";
      return;
    }
    let index = 0;
    function showPost() {
      const post = posts[index];
      const card = clone("careme-post");
      const date = card.querySelector("time");
      date.dateTime = post.date;
      date.textContent = new Date(post.date).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
      const text = card.querySelector("p");
      for (const part of post.text.split(/(https?:\/\/[^\s]+)/g)) {
        const node = document.createTextNode(part);
        if (!/^https?:\/\//.test(part)) text.append(node);
        else {
          const link = document.createElement("a");
          link.href = part;
          link.target = "_blank";
          link.rel = "noreferrer";
          link.className = "text-brand-700 underline underline-offset-4";
          link.append(node);
          text.append(link);
        }
      }
      for (const media of post.media) {
        const url = media.src;
        if (media.type.startsWith("image/")) {
          const photo = clone("careme-photo");
          photo.href = url;
          photo.querySelector("img").src = url;
          photo.querySelector("img").alt = media.alt;
          card.querySelector("[data-photos]").append(photo);
        } else if (media.type.startsWith("video/")) {
          const video = clone("careme-video");
          video.querySelector("source").src = url;
          video.querySelector("source").type = media.type;
          video.querySelector("a").href = url;
          card.append(video);
        }
      }
      for (const video of feed.querySelectorAll("video")) video.pause();
      feed.replaceChildren(card);
      newer.disabled = index === 0;
      older.disabled = index === posts.length - 1;
      document.getElementById("careme-feed-position").textContent = `Post ${index + 1} of ${posts.length}`;
    }
    newer.addEventListener("click", () => { if (index > 0) { index--; showPost(); } });
    older.addEventListener("click", () => { if (index < posts.length - 1) { index++; showPost(); } });
    showPost();
    controls.hidden = posts.length < 2;
    status.textContent = "";
  } catch {
    status.textContent = "Posts couldn’t load. Try refreshing the page, or follow Careme below.";
  } finally {
    feed.setAttribute("aria-busy", "false");
  }
}

initializeFeed();
