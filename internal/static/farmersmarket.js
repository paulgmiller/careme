import Compressor from "https://esm.sh/compressorjs@1.3.0";

// This module only runs on the farmers-market page. Keeping initialization in
// a function makes the asset safe to load from pages that do not have the form.
const initializeFarmersMarketUpload = () => {
  const form = document.getElementById("farmers-market-form");
  const submit = document.getElementById("farmers-market-submit");
  const status = document.getElementById("farmers-market-location-status");
  const photos = document.getElementById("photos");
  const lat = document.getElementById("lat");
  const lon = document.getElementById("lon");

  if (!form || !submit || !status || !photos || !lat || !lon) {
    return;
  }

  const maxPhotoBytes = 2 * 1024 * 1024;

  let photosPrepared = false;
  let preparing = false;

  photos.addEventListener("change", () => {
    photosPrepared = false;
  });

  const compressPhoto = (file) => {
    if (!file.type.startsWith("image/") || file.size <= maxPhotoBytes) {
      return Promise.resolve(file);
    }

    // Compressor.js decodes and recompresses one file at a time. The original
    // is retained if compression cannot make the upload smaller.
    return new Promise((resolve) => {
      new Compressor(file, {
        quality: 0.82,
        mimeType: "image/jpeg",

        success(blob) {
          if (blob.size >= file.size) {
            resolve(file);
            return;
          }

          const stem =
            file.name.replace(/\.[^.]+$/, "") || "market-photo";

          resolve(
            new File([blob], `${stem}.jpg`, {
              type: "image/jpeg",
              lastModified: file.lastModified,
            }),
          );
        },

        error() {
          // If compression fails, upload the original.
          resolve(file);
        },
      });
    });
  };

  const preparePhotos = async () => {
    if (!("DataTransfer" in window)) {
      return;
    }

    // File inputs cannot be assigned a new FileList directly. DataTransfer is
    // the browser-supported way to replace the selected files before HTMX
    // builds its multipart FormData payload.
    const selected = Array.from(photos.files || []);

    let prepared;
    try {
      prepared = new DataTransfer();
    } catch {
      return;
    }

    // Process files sequentially so compression does not decode every photo
    // into browser memory at the same time.
    for (let index = 0; index < selected.length; index += 1) {
      status.textContent =
        `Preparing photo ${index + 1} of ${selected.length}...`;

      prepared.items.add(await compressPhoto(selected[index]));
    }

    photos.files = prepared.files;
  };

  const geolocationAvailable = "geolocation" in navigator;

  const findLocation = () =>
    new Promise((resolve, reject) => {
      navigator.geolocation.getCurrentPosition(
        resolve,
        reject,
        {
          enableHighAccuracy: false,
          timeout: 10000,
          maximumAge: 300000,
        },
      );
    });

  if (!geolocationAvailable) {
    status.textContent =
      "Current location is not available in this browser.";
  }

  const setSubmitState = (disabled, text) => {
    submit.disabled = disabled;
    submit.textContent = text;
  };

  form.addEventListener(
    "submit",
    async (event) => {
      const needsPhotos = !photosPrepared;
      const needsLocation =
        geolocationAvailable &&
        (lat.value === "" || lon.value === "");

      if (!needsPhotos && !needsLocation) {
        return;
      }

      event.preventDefault();
      event.stopImmediatePropagation();

      if (preparing) {
        return;
      }

      preparing = true;
      const originalText = submit.textContent;
      setSubmitState(true, originalText);

      try {
        if (needsPhotos) {
          submit.textContent = "Preparing your photos...";
          await preparePhotos();
          photosPrepared = true;
        }

        if (needsLocation) {
          submit.textContent = "Finding your location...";
          status.textContent = "Finding your current location...";

          const position = await findLocation();

          lat.value = position.coords.latitude.toFixed(6);
          lon.value = position.coords.longitude.toFixed(6);
        }

        status.textContent = "Uploading your market photos...";
        preparing = false;

        // The first submit is intercepted above. Re-submit after preparation
        // so HTMX can collect the resized FileList and send the normal request.
        setSubmitState(false, originalText);
        form.requestSubmit();
      } catch {
        lat.value = "";
        lon.value = "";

        status.textContent = "Could not use current location.";
        preparing = false;
        setSubmitState(false, originalText);
      }
    },
    true,
  );
};

initializeFarmersMarketUpload();
