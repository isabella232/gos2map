var color0 = '#FFFF00';
var color1 = '#00FF00';

L.Control.Command = L.Control.extend({
    options: {
        position: 'topright',
    },

    onAdd: function (map) {
        var controlDiv = L.DomUtil.create('div', 'leaflet-control-command');
        L.DomEvent
            .addListener(controlDiv, 'click', L.DomEvent.stopPropagation)
            .addListener(controlDiv, 'click', L.DomEvent.preventDefault)
            .addListener(controlDiv, 'click', this.options.click);

        var controlUI = L.DomUtil.create('div', 'leaflet-control-command-interior', controlDiv);
        controlUI.title = this.options.title;
        controlUI.innerHTML = '<span style="font-size: 24px; position: relative; top:-10px;">'+this.options.text+'</span>';
        return controlDiv;
    },
});

L.control.command = function (options) {
    return new L.Control.Command(options);
};

var PageController = Backbone.Model.extend({
    geometries: [],

    showS2Covering: function() {
        return this.$s2coveringButton.is(':checked')
    },

    resetDisplay: function() {
        this.previousBounds = null;
        this.layerGroup.clearLayers();
        this.drawnItems.clearLayers();
        this.$infoArea.empty();
    },

    addInfo: function(msg) {
        this.$infoArea.append($('<div>' + msg + '</div>'));
    },

    cellDescription: function(cell) {
        return 'cell id (unsigned): ' + cell.id + '<br>' +
            'cell id (signed): ' + cell.id_signed + '<br>' +
            'cell token: ' + cell.token + '<br>' +
            //    'face: ' + cell.face + '<br>' +
            'level: ' + cell.level + '<br>' +
            'center: ' + cell.ll.lat + "," + cell.ll.lng;
    },

    /**
     * @param {fourSq.api.models.geo.S2Response} cell
     * @return {L.Polygon}
     */
    renderCell: function(cell, color, extraDesc, opacity) {
        if (!color) {
            color = color1;
        }

        opacity = opacity || 0.2;

        var description = this.cellDescription(cell)
        if (extraDesc) {
            description += '<p>' + extraDesc;
        }

        this.$infoArea.append(description);
        this.$infoArea.append('<br/>');

        var points = _(cell.shape).map(function(ll) {
            return new L.LatLng(ll.lat, ll.lng);
        });

        var geodesic = L.geodesic([points], {steps:10});
        var geodesicPoints = geodesic.getLatLngs();

        var polygon = new L.Polygon(geodesicPoints, {
            color: color,
            weight: 2,
            fill: true,
            fillOpacity: opacity
        });

        polygon.bindPopup(description);
        this.layerGroup.addLayer(polygon);
        return polygon;
    },

    /**
     * @param {Array.<fourSq.api.models.geo.S2Response>} cells
     * @return {Array.<L.Polygon>}
     */
    renderCells: function(cells) {
        return _(cells).filter(function(cell) { return cell.token != "X"; })
            .map(_.bind(function(c) {
                return this.renderCell(c);
            }, this));
    },

    renderS2Cells: function(cells) {
        var bounds = null;
        var polygons = this.renderCells(cells);
        _.each(polygons, function(p) {
            if (!bounds) {
                bounds = new L.LatLngBounds([p.getBounds()]);
            }
            bounds = bounds.extend(p.getBounds());
        });
        this.processBounds(bounds);
    },

    processBounds: function(bounds) {
        if (bounds !== null) {
            this.map.setView(bounds.getCenter(),
                             this.map.getBoundsZoom(bounds), false);
        }
    },

    renderCovering: function(latlngs) {
        if (this.showS2Covering()) {
            var data = {};
            if (latlngs['type'] && latlngs['geometry']) {
                data = {"geojson": JSON.stringify(latlngs)};
            } else {
                data = {
                    'points': _(latlngs).map(function(ll) {
                        return ll.lat + "," + ll.lng;
                    }).join(',')
                };
            }

            if (this.$minLevel.val()) {
                data['min_level'] = this.$minLevel.val();
            }
            if (this.$maxLevel.val()) {
                data['max_level'] = this.$maxLevel.val();
            }
            if (this.$maxCells.val()) {
                data['max_cells'] = this.$maxCells.val();
            }
            if (this.$levelMod.val()) {
                data['level_mod'] = this.$levelMod.val();
            }

            $.ajax({
                url: '/a/s2cover',
                type: 'POST',
                dataType: 'json',
                data: data,
                success: _.bind(this.renderS2Cells, this)
            });
        }
    },

    renderFeatureCovering: function(geojsonFeature) {
        var points = [];
        var newFeature = {
            'type': 'Feature',
            'properties': {},
            'geometry': {'type': 'Polygon', 'coordinates': []},
        }
        console.log('trying to load')
        coords = geojsonFeature['geometry']['coordinates'];
        flatcoords = _.flatten(coords);
        for (var i = 0; i < flatcoords.length; i+=2) {
            points.push(new L.LatLng(flatcoords[i+1], flatcoords[i]));
        }
        for (var i = 0; i < coords.length; i++) {
            var pts = [];
            var geomCoords = [];
            for (var j = 0; j < coords[i].length; j++) {
                var ll = new L.LatLng(coords[i][j][1], coords[i][j][0]);
                pts.push(ll);
            }
            var geodesic = L.geodesic([pts], {steps:10});
            var geodesicPoints = geodesic.getLatLngs();
            var newCoords = [];
            for (var j = 0; j < geodesicPoints[0].length; j++) {
                geomCoords.push([geodesicPoints[0][j].lng, geodesicPoints[0][j].lat]);
            }
            newFeature['geometry']['coordinates'].push(geomCoords);
        }
        this.renderCovering(newFeature);
    },

    boundsCallback: function() {
        this.setHash();
        this.resetDisplay();
        var geojsonFeature = null;
        var bboxstr = this.editor.getValue();
        try {
            console.log('trying json parse')
            geojsonFeature = JSON.parse(bboxstr);
        } catch(e) {
            console.log(e)
            console.log('could not parse')
            return;
        }
        var collection = L.geoJson(geojsonFeature, {
            style: function(f) {
                return {weight: 2, color: color0};
            }
        });
        collection.eachLayer(_.bind(this.addDrawnLayer, this));
    },

    addDrawnLayer: function(l) {
        if (this.previousBounds === null || this.previousBounds === undefined) {
            this.previousBounds = l.getBounds();
        }
        this.previousBounds = this.previousBounds.extend(l.getBounds());
        this.drawnItems.addLayer(l);
        this.processBounds(this.previousBounds);
        if (this.showS2Covering()) {
            this.renderFeatureCovering(l.toGeoJSON());
        }
    },

    updateS2CoverMode: function() {
        if (this.showS2Covering()) {
            this.$s2options.show();
        } else {
            this.$s2options.hide();
        }
    },

    showSetOperators: function() {
        if (this.setOpsVisible === undefined || !this.setOpsVisible) {
            this.setOpsVisible = true;
            this.map.addControl(this.union);
            this.map.addControl(this.intersection);
            this.map.addControl(this.difference);
        }
    },

    hideSetOperators: function() {
        try {
            this.setOpsVisible = false;
            this.map.removeControl(this.union);
            this.map.removeControl(this.intersection);
            this.map.removeControl(this.difference);
        } catch (err) {}
    },

    operationCallback: function(data) {
        this.previousBounds = null;
        this.drawnItems.clearLayers();
        this.hideSetOperators();
        this.editor.setValue(JSON.stringify(data, null, 2));
        this.boundsCallback();
        this.setHash();
    },

    initialize: function() {
        this.editor = CodeMirror.fromTextArea(document.getElementById('textarea'), {
            lineNumbers: true,
        });

        this.editor.setValue(JSON.stringify({
            "type": "FeatureCollection", "features": []
        }, null, 2));

        this.editor.on('change', _.bind(function(doc, obj) {
            console.log(obj);
            if (obj.origin == "setValue") return;
            if (obj.origin == "+delete") {
                if (doc.getValue().length == 0) {
                    doc.setValue(JSON.stringify({
                        "type": "FeatureCollection", "features": []
                    }, null, 2));
                }
            }
            this.boundsCallback();
        }, this));

        this.union = L.control.command({
            text: '&#x22C3;',
            title: 'Set Union',
            click: _.bind(function() {
                $.post("/a/union", JSON.stringify({"geoms":this.drawnItems.toGeoJSON()}),
                       _.bind(this.operationCallback, this));
            }, this)
        });

        this.intersection = L.control.command({
            text: '&#x22C2;',
            title: 'Set Intersection',
            click: _.bind(function() {
                $.post("/a/intersection", JSON.stringify({"geoms":this.drawnItems.toGeoJSON()}),
                       _.bind(this.operationCallback, this));
            }, this)
        });

        this.difference = L.control.command({
            text: '&#x2212;',
            title: 'Set Difference',
            click: _.bind(function() {
                $.post("/a/difference", JSON.stringify({"geoms":this.drawnItems.toGeoJSON()}),
                       _.bind(this.operationCallback, this));
            }, this)
        });

        var opts = {
            attributionControl: false,
            zoomControl: false,
        }

        L.mapbox.accessToken = 'pk.eyJ1IjoiZGF2aWRyZXlub2xkcyIsImEiOiJlZkpBeXBFIn0.S4DYvY-hIfKnvhfdXMuH5A';
        this.map = L.mapbox.map('map', 'davidreynolds.lefk0pn0', opts);

        var zoom = new L.Control.Zoom()
        zoom.setPosition('topright');
        this.map.addControl(zoom);

        this.drawnFeatureCollection = [];
        this.drawnItems = new L.FeatureGroup();
        this.map.addLayer(this.drawnItems);

        var drawControl = new L.Control.Draw({
            position: 'topright',
            draw: {
                marker: null,
                circle: {
                    shapeOptions: {
                        weight: 2,
                        color: color0,
                    }
                },
                polyline: null,
                rectangle: {
                    shapeOptions: {
                        weight: 2,
                        color: color0,
                    }
                },
                polygon: {
                    allowIntersection: false,
                    drawError: {
                        color: '#b00b00',
                        timeout: 1000
                    },
                    shapeOptions: {
                        weight: 2,
                        color: color0
                    },
                    showArea: true
                },
            },
            edit: {
                featureGroup: this.drawnItems
            }
        });
        this.map.addControl(drawControl);

        this.map.on('draw:created', _.bind(this.drawCreatedCallback, this));
        this.map.on('draw:deleted', _.bind(this.drawDeletedCallback, this));
        this.map.on('draw:edited', _.bind(this.drawEditedCallback, this));

        this.attribution = new L.Control.Attribution();
        this.attribution.addAttribution('<a target="_blank" href="https://github.com/davidreynolds/gos2">Powered by S2</a>');
        this.map.addControl(this.attribution);

        // For coverings.
        this.layerGroup = new L.LayerGroup();
        this.map.addLayer(this.layerGroup);
        this.map.on('click', _.bind(function(e) {
            if (e.originalEvent.metaKey ||
                e.originalEvent.altKey ||
                e.originalEvent.ctrlKey) {
                var popup = L.popup()
                    .setLatLng(e.latlng)
                    .setContent(e.latlng.lat + ',' + e.latlng.lng)
                    .openOn(this.map);
            }
        }, this));

        this.$el = $(document);
        this.$infoArea = this.$el.find('.info');

        this.$boundsButton = this.$el.find('.boundsButton');
        this.$boundsButton.click(_.bind(this.boundsCallback, this));

        this.$s2options = this.$el.find('.s2options');
        this.$s2coveringButton = this.$el.find('.s2cover');
        this.$s2coveringButton.change(_.bind(function() {
            this.updateS2CoverMode();
            this.setHash();
            this.boundsCallback();
        }, this));

        this.$maxCells = this.$el.find('.max_cells');
        this.$maxLevel = this.$el.find('.max_level');
        this.$minLevel = this.$el.find('.min_level');
        this.$levelMod = this.$el.find('.level_mod');

        // https://github.com/blackmad/s2map
    },

    drawDeletedCallback: function(e) {
    },

    drawEditedCallback: function(e) {
    },

    drawCreatedCallback: function(e) {
        var type = e.layerType,
            layer = e.layer;
        if (type === 'polygon' || type === 'rectangle' || type === 'circle') {
            if (type == 'circle') {
                layer = LGeo.circle(layer.getLatLng(), layer.getRadius(), {
                    color: color0,
                    weight: 2,
                    fill: true,
                    fillOpacity: 0.2,
                });
            }
            this.addDrawnLayer(layer);
            this.editor.setValue(JSON.stringify(this.drawnItems.toGeoJSON(), null, 2));
            this.setHash();
            if (this.drawnItems.getLayers().length >= 2) {
                this.showSetOperators();
            } else {
                this.hideSetOperators();
            }
        }
    },

    initMapPage: function() {
        this.parseHash(window.location.hash.substring(1) || window.location.search.substring(1));
        this.updateS2CoverMode();
        this.boundsCallback();
    },

    setHash: function(tokens) {
        var h = "";
        function addParam(k, v) {
            if (h != "") {
                h+= "&"
            }
            h += k + "=" + v;
        }

        if (this.showS2Covering()) {
            addParam("s2", 'true');
            addParam("s2_min_level", this.$minLevel.val());
            addParam("s2_max_level", this.$maxLevel.val());
            addParam("s2_max_cells", this.$maxCells.val());
            addParam("s2_level_mod", this.$levelMod.val());
        } else {
            addParam("s2", 'false');
        }
        addParam("geojson", JSON.stringify(JSON.parse(this.editor.getValue())));
        window.location.hash = h;
    },

    deparam: function (querystring) {
        // remove any preceding url and split
        querystring = querystring.substring(querystring.indexOf('?')+1).split('&');
        var params = {}, pair, d = decodeURIComponent;
        // march and parse
        for (var i = querystring.length - 1; i >= 0; i--) {
            pair = querystring[i].split('=');
            params[d(pair[0])] = d(pair[1]);
        }
        return params;
    },

    parseHash: function(hash) {
        if (hash.indexOf('=') == -1) {
            return;
        }

        var params = this.deparam(hash);
        this.updateS2CoverMode();

        if (params.s2 == 'true') {
            this.$s2coveringButton.attr('checked', 'checked');
        }

        this.$maxCells.val(params.max_cells);
        this.$minLevel.val(params.min_level);
        this.$maxLevel.val(params.max_level);
        this.$levelMod.val(params.level_mod);
        this.editor.setValue(JSON.stringify(JSON.parse(params.geojson), null, 2));
    },
});
