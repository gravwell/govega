function makesvg(spec, data) {
  try {
    // If it's a string, parse it. Otherwise it's probably an object and we're good to go.
    const specobj =
      typeof spec === "string" || spec instanceof String
        ? JSON.parse(spec)
        : spec;

    // We marshalled this to JSON, we should be able to parse it
    const dataObj = data ? JSON.parse(data) : {};
    //if there are no data members at all, just set the data
    const d = specobj.data;
    if (!d) {
      specobj.data = Object.keys(dataObj).map((k) => ({
        name: k,
        values: dataObj[k],
      }));
    } else {
      //do a merge of what exists in the spec with what is in the data object
      specobj.data = specobj.data.map((table) => {
        if (table.name in dataObj) {
          return { ...table, values: dataObj[table.name] };
        } else {
          return table;
        }
      });
      for (let name in dataObj) {
        const table = specobj.data.find((d) => d.name === name);
        if (!table) {
          specobj.data.push({
            name: name,
            values: dataObj[name],
          });
        }
      }
    }

    // Create the view
    const view = new vega.View(vega.parse(specobj), { renderer: "none" });

    // If cxt (canvas context) is set, use Canvas, otherwise SVG
    const renderPromise = cxt
      ? view.toCanvas(1, { externalContext: cxt })
      : view.toSVG();

    // Render the view
    renderPromise
      .then((result) => {
        success(result);
      })
      .catch((err) => {
        failure(err.toString());
      })
      .finally(() => {
        view.finalize();
      });
  } catch (err) {
    failure(err.toString());
  }
  return true; //return true as a clean completion schedule
}
